package services

import (
	"context"
	"fmt"
	"strings"

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

// RunAndWait waits for the requested command rather than every command in the runtime queue.
func (s *RuntimeSupervisor) RunAndWait(id string, command string) (*domain.RuntimeScroll, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	if err := session.AddTempItemWithWait(command); err != nil {
		return nil, err
	}
	return s.store.GetScroll(id)
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
	unlock := s.lockRuntimeOperation(id)
	defer unlock()
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	wasRunning := sessionWasRunning(session)
	if err := session.StopRuntimeForMaintenance(); err != nil {
		session.markError(err)
		return nil, err
	}
	if err := session.Backup(context.Background(), artifact, registryCredentials); err != nil {
		if wasRunning {
			if _, restartErr := s.startScroll(id); restartErr != nil {
				return nil, fmt.Errorf("backup failed: %w; failed to restart prior runtime: %v", err, restartErr)
			}
			return nil, err
		}
		session.markError(err)
		return nil, err
	}
	if wasRunning {
		return s.startScroll(id)
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSupervisor) Restore(id string, artifact string, restart bool, registryCredentials []domain.RegistryCredential) (*domain.RuntimeScroll, error) {
	unlock := s.lockRuntimeOperation(id)
	defer unlock()
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	root := session.runtimeScroll.Root
	session.mu.Unlock()
	wasRunning := sessionWasRunning(session)
	if err := session.StopRuntimeForMaintenance(); err != nil {
		session.markError(err)
		return nil, err
	}
	materialized, err := s.runPullWorker(context.Background(), s.runtimeBackend, ports.RuntimeWorkerModeRestore, id, artifact, root, registryCredentials, "")
	if err != nil {
		if wasRunning && restoreFailureCanRestart(err) {
			if _, restartErr := s.startScroll(id); restartErr != nil {
				return nil, fmt.Errorf("restore failed: %w; failed to restart prior runtime: %v", err, restartErr)
			}
			return nil, err
		}
		session.markError(err)
		return nil, err
	}
	if err := session.ApplyRestore(materialized); err != nil {
		session.markError(err)
		return nil, err
	}
	if restart {
		return s.startScroll(id)
	}
	return s.store.GetScroll(id)
}

func sessionWasRunning(session *RuntimeSession) bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.runtimeScroll.Status == domain.RuntimeScrollStatusRunning
}

func restoreFailureCanRestart(err error) bool {
	return !strings.Contains(err.Error(), "restore root may be partial:")
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
