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
	s.mu.Unlock()
	return s.runtimeBackend.ExpectedPorts(runtimeScroll.Root, s.scrollService.GetFile().Commands, s.scrollService.GetFile().Ports)
}

func (s *RuntimeSession) RoutingTargets() ([]domain.RuntimeRoutingTarget, error) {
	s.mu.Lock()
	runtimeScroll := *s.runtimeScroll
	s.mu.Unlock()
	return s.runtimeBackend.RoutingTargets(runtimeScroll.Root, s.scrollService.GetFile().Commands, s.scrollService.GetFile().Ports)
}

func (s *RuntimeSession) Procedures() map[string]domain.ScrollLockStatus {
	return s.procedures.GetProcedureStatuses()
}

func (s *RuntimeSession) ApplyRouting(assignments []domain.RuntimeRouteAssignment) (*domain.RuntimeScroll, error) {
	s.mu.Lock()
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

func (s *RuntimeSession) StopRuntime() error {
	s.mu.Lock()
	root := s.runtimeScroll.Root
	s.mu.Unlock()
	if err := s.runtimeBackend.StopRuntime(root); err != nil {
		return err
	}
	s.mu.Lock()
	commands := s.scrollService.GetFile().Commands
	for commandName, status := range s.runtimeScroll.Commands {
		command := commands[commandName]
		if command != nil && command.Run == domain.RunModeOnce && status.Status == domain.ScrollLockStatusDone {
			continue
		}
		delete(s.runtimeScroll.Commands, commandName)
	}
	s.runtimeScroll.Status = domain.RuntimeScrollStatusStopped
	s.runtimeScroll.LastError = ""
	err := s.store.UpdateScroll(s.runtimeScroll)
	s.mu.Unlock()
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
	processLauncher, err := coreservices.NewProcedureLauncherForRuntime(scrollService, s.runtimeBackend, root, s.runtimeScroll.ID, s.runtimeScroll.ScrollName, func() []domain.RuntimeRouteAssignment {
		s.mu.Lock()
		defer s.mu.Unlock()
		routing := make([]domain.RuntimeRouteAssignment, len(s.runtimeScroll.Routing))
		copy(routing, s.runtimeScroll.Routing)
		return routing
	})
	if err != nil {
		return err
	}
	queueManager := coreservices.NewQueueManager(scrollService, processLauncher)
	queueManager.SetStatusObserver(s.persistCommandStatus)

	s.mu.Lock()
	oldQueue := s.queueManager
	commands := scrollService.GetFile().Commands
	routing := preserveRoutingAssignments(s.runtimeScroll.Routing, scrollService.GetFile().Ports)
	for commandName := range s.runtimeScroll.Commands {
		if commands[commandName] == nil {
			delete(s.runtimeScroll.Commands, commandName)
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
	s.queueManager = queueManager
	s.procedures = processLauncher
	started := s.started
	err = s.store.UpdateScroll(s.runtimeScroll)
	s.mu.Unlock()
	if err == nil && started {
		oldQueue.Shutdown()
		go queueManager.Work()
	}
	return err
}
