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
	statuses := copyCommandStatuses(s.runtimeScroll.Commands)
	runtimeStatus := s.runtimeScroll.Status
	s.mu.Unlock()
	commands := s.scrollService.GetFile().Commands
	if len(statuses) > 0 {
		filtered := map[string]domain.LockStatus{}
		removedStaleStatus := false
		for commandName, status := range statuses {
			command := commands[commandName]
			if command == nil {
				removedStaleStatus = true
				continue
			}
			// Kubernetes keeps persistent workloads alive; do not requeue them just because
			// the singleton API process restarted.
			if runtimeStatus == domain.RuntimeScrollStatusRunning && status.Status == domain.ScrollLockStatusDone && command.Run == domain.RunModePersistent {
				continue
			}
			filtered[commandName] = status
		}
		if removedStaleStatus {
			s.mu.Lock()
			for commandName := range s.runtimeScroll.Commands {
				if commands[commandName] == nil {
					delete(s.runtimeScroll.Commands, commandName)
				}
			}
			err := s.store.UpdateScroll(s.runtimeScroll)
			s.mu.Unlock()
			if err != nil {
				return err
			}
		}
		statuses = filtered
		if err := s.queueManager.HydrateCommandStatuses(statuses); err != nil {
			return err
		}
	}
	if err := s.AutoStartServe(); err != nil {
		return err
	}
	s.mu.Lock()
	s.runtimeScroll.Status = deriveRuntimeScrollStatus(s.runtimeScroll.Commands, s.scrollService.GetFile().Commands)
	err := s.store.UpdateScroll(s.runtimeScroll)
	s.mu.Unlock()
	return err
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
		status, ok := s.runtimeScroll.Commands[serveCommand]
		runtimeStatus := s.runtimeScroll.Status
		s.mu.Unlock()
		if ok && status.Status == domain.ScrollLockStatusDone && runtimeStatus == domain.RuntimeScrollStatusRunning {
			return nil
		}
	}
	if err := s.queueManager.AddForcedItem(serveCommand); err != nil && !errors.Is(err, coreservices.ErrAlreadyInQueue) {
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

	if err := s.queueManager.AddTempItem(command); err != nil {
		s.markError(err)
		return nil, err
	}
	if !longRunning {
		if err := s.queueManager.WaitUntilEmptyContext(ctx); err != nil {
			s.markError(err)
			return nil, err
		}
	}

	s.mu.Lock()
	s.runtimeScroll.Status = deriveRuntimeScrollStatus(s.runtimeScroll.Commands, s.scrollService.GetFile().Commands)
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
	for commandName := range fresh.Commands {
		if commands[commandName] == nil {
			delete(fresh.Commands, commandName)
			removedStaleStatus = true
		}
	}
	if removedStaleStatus {
		_ = s.store.UpdateScroll(fresh)
	}
	s.mu.Lock()
	s.runtimeScroll.Commands = copyCommandStatuses(fresh.Commands)
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
		status, ok := s.runtimeScroll.Commands[dependency]
		s.mu.Unlock()
		if ok && status.Status == domain.ScrollLockStatusDone {
			s.queueManager.RememberDoneItem(dependency)
		}
		dependencyCommand, err := s.scrollService.GetCommand(dependency)
		if err == nil {
			s.rememberDoneDependencies(dependencyCommand, seen)
		}
	}
}

func (s *RuntimeSession) persistCommandStatus(command string, status domain.ScrollLockStatus, exitCode *int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	commands := s.scrollService.GetFile().Commands
	if commands[command] == nil {
		return
	}
	if s.runtimeScroll.Commands == nil {
		s.runtimeScroll.Commands = map[string]domain.LockStatus{}
	}
	for commandName := range s.runtimeScroll.Commands {
		if commands[commandName] == nil {
			delete(s.runtimeScroll.Commands, commandName)
		}
	}
	s.runtimeScroll.Commands[command] = domain.LockStatus{
		Status:           status,
		ExitCode:         exitCode,
		LastStatusChange: time.Now().Unix(),
	}
	s.runtimeScroll.Status = deriveRuntimeScrollStatus(s.runtimeScroll.Commands, s.scrollService.GetFile().Commands)
	if err := s.store.UpdateScroll(s.runtimeScroll); err != nil {
		logger.Log().Error("failed to persist command status", zap.String("scroll", s.runtimeScroll.ID), zap.String("command", command), zap.Error(err))
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
