package services

import (
	"context"
	"errors"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (s *RuntimeSession) Hydrate() error {
	s.mu.Lock()
	statuses := copyProcedureStatuses(s.runtimeScroll.Procedures)
	runtimeStatus := s.runtimeScroll.Status
	s.mu.Unlock()
	commands := s.scrollService.GetFile().Commands
	if len(statuses) > 0 {
		filtered := domain.ProcedureStatusMap{}
		removedStaleStatus := false
		for commandName, procedureStatuses := range statuses {
			command := commands[commandName]
			if command == nil {
				removedStaleStatus = true
				continue
			}
			filteredProcedures := map[string]domain.LockStatus{}
			for idx, procedure := range command.Procedures {
				procedureName := domain.ProcedureName(commandName, idx, procedure)
				if status, ok := procedureStatuses[procedureName]; ok {
					filteredProcedures[procedureName] = status
				}
			}
			if len(filteredProcedures) != len(procedureStatuses) {
				removedStaleStatus = true
			}
			// Kubernetes keeps persistent workloads alive; do not requeue them just because
			// the singleton API process restarted.
			commandStatus, ok := deriveCommandStatus(filteredProcedures, commandName, command)
			if ok && runtimeStatus == domain.RuntimeScrollStatusRunning && commandStatus == domain.ScrollLockStatusRunning && command.Run == domain.RunModePersistent {
				continue
			}
			if len(filteredProcedures) > 0 {
				filtered[commandName] = filteredProcedures
			}
		}
		if removedStaleStatus {
			s.mu.Lock()
			for commandName := range s.runtimeScroll.Procedures {
				if commands[commandName] == nil {
					delete(s.runtimeScroll.Procedures, commandName)
					continue
				}
				for procedureName := range s.runtimeScroll.Procedures[commandName] {
					found := false
					for idx, procedure := range commands[commandName].Procedures {
						if procedureName == domain.ProcedureName(commandName, idx, procedure) {
							found = true
							break
						}
					}
					if !found {
						delete(s.runtimeScroll.Procedures[commandName], procedureName)
					}
				}
			}
			err := s.store.UpdateScroll(s.runtimeScroll)
			s.mu.Unlock()
			if err != nil {
				return err
			}
		}
		statuses = filtered
		if err := s.HydrateFromState(statuses); err != nil {
			return err
		}
	}
	if err := s.AutoStartServe(); err != nil {
		return err
	}
	s.mu.Lock()
	s.runtimeScroll.Status = deriveRuntimeScrollStatus(s.runtimeScroll.Procedures, s.scrollService.GetFile().Commands)
	err := s.store.UpdateScroll(s.runtimeScroll)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.triggerRunQueue()
	return nil
}

func (s *RuntimeSession) AutoStartServe() error {
	serveCommand := s.scrollService.GetFile().Serve
	if serveCommand == "" {
		return nil
	}
	command := s.scrollService.GetFile().Commands[serveCommand]
	if command == nil {
		return nil
	}
	s.rememberDoneDependencies(command, map[string]bool{})
	if command.Run == domain.RunModePersistent {
		s.mu.Lock()
		status, ok := deriveCommandStatus(s.runtimeScroll.Procedures[serveCommand], serveCommand, command)
		runtimeStatus := s.runtimeScroll.Status
		s.mu.Unlock()
		if ok && (status == domain.ScrollLockStatusDone || status == domain.ScrollLockStatusRunning) && runtimeStatus == domain.RuntimeScrollStatusRunning {
			return nil
		}
	}
	if err := s.AddForcedItem(serveCommand); err != nil && !errors.Is(err, coreservices.ErrAlreadyInQueue) {
		return err
	}
	return nil
}

func (s *RuntimeSession) Run(command string) (*domain.RuntimeScroll, error) {
	return s.RunWithContext(context.Background(), command)
}

func (s *RuntimeSession) RunWithContext(ctx context.Context, command string) (*domain.RuntimeScroll, error) {
	s.refreshCommandState()
	targetCommand, err := s.scrollService.GetCommand(command)
	if err != nil {
		s.markError(err)
		return nil, err
	}
	longRunning := targetCommand.Run == domain.RunModeRestart || targetCommand.Run == domain.RunModePersistent
	s.rememberDoneDependencies(targetCommand, map[string]bool{})

	if err := s.AddTempItem(command); err != nil {
		s.markError(err)
		return nil, err
	}
	if !longRunning {
		if err := s.WaitUntilEmptyContext(ctx); err != nil {
			s.markError(err)
			return nil, err
		}
	}

	s.mu.Lock()
	s.runtimeScroll.Status = deriveRuntimeScrollStatus(s.runtimeScroll.Procedures, s.scrollService.GetFile().Commands)
	err = s.store.UpdateScroll(s.runtimeScroll)
	id := s.runtimeScroll.ID
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSession) refreshCommandState() {
	fresh, err := s.store.GetScroll(s.runtimeScroll.ID)
	if err != nil {
		return
	}
	commands := s.scrollService.GetFile().Commands
	removedStaleStatus := false
	for commandName := range fresh.Procedures {
		if commands[commandName] == nil {
			delete(fresh.Procedures, commandName)
			removedStaleStatus = true
		}
	}
	if removedStaleStatus {
		_ = s.store.UpdateScroll(fresh)
	}
	s.mu.Lock()
	s.runtimeScroll.Procedures = copyProcedureStatuses(fresh.Procedures)
	s.runtimeScroll.Status = fresh.Status
	s.mu.Unlock()
}

func (s *RuntimeSession) rememberDoneDependencies(command *domain.CommandInstructionSet, seen map[string]bool) {
	if command == nil {
		return
	}
	for _, dependency := range command.Needs {
		if seen[dependency] {
			continue
		}
		seen[dependency] = true
		s.mu.Lock()
		dependencyCommand := s.scrollService.GetFile().Commands[dependency]
		status, ok := deriveCommandStatus(s.runtimeScroll.Procedures[dependency], dependency, dependencyCommand)
		s.mu.Unlock()
		if ok && status == domain.ScrollLockStatusDone {
			s.RememberDoneItem(dependency)
		}
		dependencyCommand, err := s.scrollService.GetCommand(dependency)
		if err == nil {
			s.rememberDoneDependencies(dependencyCommand, seen)
		}
	}
}

func (s *RuntimeSession) persistCommandStatus(command string, status domain.ScrollLockStatus, exitCode *int) {
	if status == domain.ScrollLockStatusRunning {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	commands := s.scrollService.GetFile().Commands
	commandDefinition := commands[command]
	if commandDefinition == nil {
		return
	}
	if s.runtimeScroll.Procedures == nil {
		s.runtimeScroll.Procedures = domain.ProcedureStatusMap{}
	}
	for commandName := range s.runtimeScroll.Procedures {
		if commands[commandName] == nil {
			delete(s.runtimeScroll.Procedures, commandName)
		}
	}
	if s.runtimeScroll.Procedures[command] == nil {
		s.runtimeScroll.Procedures[command] = map[string]domain.LockStatus{}
	}
	for idx, procedure := range commandDefinition.Procedures {
		procedureName := domain.ProcedureName(command, idx, procedure)
		s.runtimeScroll.Procedures[command][procedureName] = domain.LockStatus{
			Status:           status,
			ExitCode:         exitCode,
			LastStatusChange: time.Now().Unix(),
		}
	}
	s.runtimeScroll.Status = deriveRuntimeScrollStatus(s.runtimeScroll.Procedures, s.scrollService.GetFile().Commands)
	if err := s.store.UpdateScroll(s.runtimeScroll); err != nil {
		logger.Log().Error("failed to persist command status", zap.String("scroll", s.runtimeScroll.ID), zap.String("command", command), zap.Error(err))
	}
}

func (s *RuntimeSession) Snapshot() domain.ProcedureStatusMap {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyProcedureStatuses(s.runtimeScroll.Procedures)
}

func (s *RuntimeSession) SetCommandStatus(command string, status domain.ScrollLockStatus, exitCode *int) {
	s.persistCommandStatus(command, status, exitCode)
}

func (s *RuntimeSession) persistProcedureStatus(command string, procedure string, status domain.ScrollLockStatus, exitCode *int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	commands := s.scrollService.GetFile().Commands
	if commands[command] == nil {
		return
	}
	if s.runtimeScroll.Procedures == nil {
		s.runtimeScroll.Procedures = domain.ProcedureStatusMap{}
	}
	if s.runtimeScroll.Procedures[command] == nil {
		s.runtimeScroll.Procedures[command] = map[string]domain.LockStatus{}
	}
	s.runtimeScroll.Procedures[command][procedure] = domain.LockStatus{
		Status:           status,
		ExitCode:         exitCode,
		LastStatusChange: time.Now().Unix(),
	}
	s.runtimeScroll.Status = deriveRuntimeScrollStatus(s.runtimeScroll.Procedures, commands)
	if err := s.store.UpdateScroll(s.runtimeScroll); err != nil {
		logger.Log().Error("failed to persist procedure status", zap.String("scroll", s.runtimeScroll.ID), zap.String("command", command), zap.String("procedure", procedure), zap.Error(err))
	}
}

func (s *RuntimeSession) markError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeScroll.Status = domain.RuntimeScrollStatusError
	if err != nil {
		s.runtimeScroll.LastError = err.Error()
	}
	_ = s.store.UpdateScroll(s.runtimeScroll)
}
