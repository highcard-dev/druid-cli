package services

import (
	"context"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

func (s *RuntimeSupervisor) Run(id string, command string) (*domain.RuntimeScroll, error) {
	return s.RunWithContext(context.Background(), id, command)
}

func (s *RuntimeSupervisor) RunWithContext(ctx context.Context, id string, command string) (*domain.RuntimeScroll, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	return session.RunWithContext(ctx, command)
}

func (s *RuntimeSupervisor) Ports(id string) ([]domain.RuntimePortStatus, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	return session.Ports()
}

func (s *RuntimeSupervisor) RoutingTargets(id string) ([]domain.RuntimeRoutingTarget, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	return session.RoutingTargets()
}

func (s *RuntimeSupervisor) ApplyRouting(id string, assignments []domain.RuntimeRouteAssignment) (*domain.RuntimeScroll, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	return session.ApplyRouting(assignments)
}

func (s *RuntimeSupervisor) Backup(id string, artifact string, registryCredentials []domain.RegistryCredential) (*domain.RuntimeScroll, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	if err := session.Backup(context.Background(), artifact, registryCredentials); err != nil {
		session.markError(err)
		return nil, err
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSupervisor) Restore(id string, artifact string, restart bool, registryCredentials []domain.RegistryCredential) (*domain.RuntimeScroll, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	root := session.runtimeScroll.Root
	session.mu.Unlock()
	if err := session.StopRuntime(); err != nil {
		session.markError(err)
		return nil, err
	}
	materialized, err := s.runPullWorker(context.Background(), s.runtimeBackend, ports.RuntimeWorkerModeRestore, id, artifact, root, registryCredentials)
	if err != nil {
		session.markError(err)
		return nil, err
	}
	if err := session.ApplyRestore(materialized); err != nil {
		session.markError(err)
		return nil, err
	}
	if restart {
		return s.StartScroll(id)
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSupervisor) ScrollFile(id string) (*domain.File, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	return session.scrollService.GetFile(), nil
}

func (s *RuntimeSupervisor) Queue(id string) (domain.ProcedureStatusMap, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	return session.Queue(), nil
}
