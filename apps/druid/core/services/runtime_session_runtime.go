package services

import (
	"context"
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

func (s *RuntimeSession) Ports() ([]domain.RuntimePortStatus, error) {
	s.mu.Lock()
	runtimeScroll := *s.runtimeScroll
	routing := append([]domain.RuntimeRouteAssignment(nil), s.runtimeScroll.Routing...)
	s.mu.Unlock()
	file := s.scrollService.GetFile()
	runtimePorts, err := resolveRuntimePorts(file.Ports, routing, false)
	if err != nil {
		return nil, err
	}
	return s.runtimeBackend.ExpectedPorts(runtimeScroll.Root, file.Commands, runtimePorts)
}

func (s *RuntimeSession) RoutingTargets() ([]domain.RuntimeRoutingTarget, error) {
	s.mu.Lock()
	runtimeScroll := *s.runtimeScroll
	s.mu.Unlock()
	return s.runtimeBackend.RoutingTargets(runtimeScroll.Root, s.scrollService.GetFile().Commands, s.scrollService.GetFile().Ports)
}

func (s *RuntimeSession) Queue() domain.ProcedureStatusMap {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyProcedureStatuses(s.runtimeScroll.Procedures)
}

func (s *RuntimeSession) ApplyRouting(assignments []domain.RuntimeRouteAssignment) (*domain.RuntimeScroll, error) {
	s.mu.Lock()
	if s.runtimeScroll.Status == domain.RuntimeScrollStatusRunning || hasRunningProcedure(s.runtimeScroll.Procedures) {
		if err := validateDynamicPortsUnchanged(s.scrollService.GetFile().Ports, s.runtimeScroll.Routing, assignments); err != nil {
			s.mu.Unlock()
			return nil, err
		}
	}
	s.runtimeScroll.Routing = assignments
	s.runtimeScroll.LastError = ""
	err := s.store.UpdateScroll(s.runtimeScroll)
	id := s.runtimeScroll.ID
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.store.GetScroll(id)
}

func hasRunningProcedure(procedures domain.ProcedureStatusMap) bool {
	for _, commandProcedures := range procedures {
		for _, procedure := range commandProcedures {
			if procedure.Status == domain.ScrollLockStatusRunning {
				return true
			}
		}
	}
	return false
}

func (s *RuntimeSession) StopRuntime() error {
	s.mu.Lock()
	root := s.runtimeScroll.Root
	started := s.started
	s.mu.Unlock()
	if started {
		s.mu.Lock()
		s.started = false
		s.mu.Unlock()
		s.drainQueueWork()
	}
	s.resetQueueState()
	if err := s.runtimeBackend.StopRuntime(root); err != nil {
		if started {
			s.mu.Lock()
			s.started = true
			s.mu.Unlock()
			s.triggerRunQueue()
		}
		return err
	}
	s.mu.Lock()
	commands := s.scrollService.GetFile().Commands
	for commandName, procedures := range s.runtimeScroll.Procedures {
		command := commands[commandName]
		status, ok := deriveCommandStatus(procedures, commandName, command)
		if ok && command != nil && command.Run == domain.RunModeOnce && status == domain.ScrollLockStatusDone {
			continue
		}
		delete(s.runtimeScroll.Procedures, commandName)
	}
	s.runtimeScroll.Status = domain.RuntimeScrollStatusStopped
	s.runtimeScroll.LastError = ""
	err := s.store.UpdateScroll(s.runtimeScroll)
	s.mu.Unlock()
	if err == nil && started {
		s.mu.Lock()
		s.started = true
		s.mu.Unlock()
		s.triggerRunQueue()
	}
	return err
}

func (s *RuntimeSession) DeleteRuntime(purgeData bool) error {
	s.mu.Lock()
	root := s.runtimeScroll.Root
	s.mu.Unlock()
	return s.runtimeBackend.DeleteRuntime(root, purgeData)
}

func (s *RuntimeSession) Backup(ctx context.Context, artifact string, registryCredentials []domain.RegistryCredential) error {
	s.mu.Lock()
	root := s.runtimeScroll.Root
	s.mu.Unlock()
	return s.runtimeBackend.BackupRuntime(ctx, root, artifact, registryCredentials)
}

func (s *RuntimeSession) ApplyRestore(materialized *ports.RuntimeMaterialization) error {
	if materialized == nil {
		return fmt.Errorf("restore materialization is required")
	}
	if materialized.Root == "" {
		return fmt.Errorf("restore materialization has no root")
	}
	if len(materialized.ScrollYAML) == 0 {
		return fmt.Errorf("restore materialization has no scroll_yaml")
	}
	root := materialized.Root
	scrollYAML := materialized.ScrollYAML
	scrollService, err := coreservices.NewCachedScrollService(root, scrollYAML)
	if err != nil {
		return err
	}
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if started {
		s.mu.Lock()
		s.started = false
		s.mu.Unlock()
		s.drainQueueWork()
	}

	s.mu.Lock()
	commands := scrollService.GetFile().Commands
	routing := preserveRoutingAssignments(s.runtimeScroll.Routing, scrollService.GetFile().Ports)
	for commandName := range s.runtimeScroll.Procedures {
		if commands[commandName] == nil {
			delete(s.runtimeScroll.Procedures, commandName)
		}
	}
	s.runtimeScroll.Artifact = materialized.Artifact
	s.runtimeScroll.ArtifactDigest = materialized.ArtifactDigest
	s.runtimeScroll.Root = root
	s.runtimeScroll.ScrollName = scrollService.GetCurrent().Name
	s.runtimeScroll.ScrollYAML = string(scrollYAML)
	s.runtimeScroll.Routing = routing
	s.runtimeScroll.Status = domain.RuntimeScrollStatusStopped
	s.runtimeScroll.LastError = ""
	s.scrollService = scrollService
	err = s.store.UpdateScroll(s.runtimeScroll)
	s.mu.Unlock()
	if err == nil && started {
		s.resetQueueState()
		s.mu.Lock()
		s.started = true
		s.mu.Unlock()
		s.triggerRunQueue()
	}
	return err
}
