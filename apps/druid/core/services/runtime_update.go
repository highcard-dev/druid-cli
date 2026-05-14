package services

import (
	"context"
	"errors"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

func (s *RuntimeSupervisor) updateExistingScroll(runtimeScroll *domain.RuntimeScroll, artifact string, knownDigest string, registryCredentials []domain.RegistryCredential) (*domain.RuntimeScroll, error) {
	s.mu.Lock()
	session := s.sessions[runtimeScroll.ID]
	delete(s.sessions, runtimeScroll.ID)
	s.mu.Unlock()
	if session != nil {
		session.Shutdown()
	}

	if runtimeScroll.Status == domain.RuntimeScrollStatusRunning {
		if err := s.runtimeBackend.StopRuntime(runtimeScroll.Root); err != nil {
			runtimeScroll.Status = domain.RuntimeScrollStatusError
			runtimeScroll.LastError = err.Error()
			_ = s.store.UpdateScroll(runtimeScroll)
			return nil, err
		}
	}

	if s.workerCallbacks == nil || s.workerCallbackURL == "" {
		err := errors.New("worker callback URL is required for daemon update")
		runtimeScroll.Status = domain.RuntimeScrollStatusError
		runtimeScroll.LastError = err.Error()
		_ = s.store.UpdateScroll(runtimeScroll)
		return nil, err
	}
	materialized, err := s.runPullWorker(context.Background(), s.runtimeBackend, ports.RuntimeWorkerModeUpdate, runtimeScroll.ID, artifact, runtimeScroll.Root, registryCredentials)
	if err != nil {
		runtimeScroll.Status = domain.RuntimeScrollStatusError
		runtimeScroll.LastError = err.Error()
		_ = s.store.UpdateScroll(runtimeScroll)
		return nil, err
	}
	scroll, err := domain.NewScrollFromBytes(materialized.Root, materialized.ScrollYAML)
	if err != nil {
		return nil, err
	}
	if err := scroll.Validate(false); err != nil {
		return nil, err
	}
	runtimeScroll.Artifact = materialized.Artifact
	if runtimeScroll.Artifact == "" {
		runtimeScroll.Artifact = artifact
	}
	runtimeScroll.ArtifactDigest = materialized.ArtifactDigest
	if runtimeScroll.ArtifactDigest == "" {
		runtimeScroll.ArtifactDigest = knownDigest
	}
	runtimeScroll.Root = materialized.Root
	runtimeScroll.ScrollName = scroll.Name
	runtimeScroll.ScrollYAML = string(materialized.ScrollYAML)
	runtimeScroll.Commands = map[string]domain.LockStatus{}
	runtimeScroll.Routing = nil
	runtimeScroll.LastError = ""
	if runtimeScroll.Status == domain.RuntimeScrollStatusRunning || runtimeScroll.Status == domain.RuntimeScrollStatusStopped {
		runtimeScroll.Status = domain.RuntimeScrollStatusStopped
	} else {
		runtimeScroll.Status = domain.RuntimeScrollStatusCreated
	}
	if err := s.store.UpdateScroll(runtimeScroll); err != nil {
		return nil, err
	}
	return s.store.GetScroll(runtimeScroll.ID)
}
