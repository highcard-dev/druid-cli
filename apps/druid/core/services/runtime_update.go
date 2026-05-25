package services

import (
	"context"
	"errors"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (s *RuntimeSupervisor) Update(id string, artifact string, registryCredentials []domain.RegistryCredential) (*domain.RuntimeScroll, error) {
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	if artifact == "" {
		artifact = runtimeScroll.Artifact
	}
	knownDigest := resolveArtifactDigest(artifact, registryCredentials)
	return s.updateExistingScroll(runtimeScroll, artifact, knownDigest, registryCredentials, true)
}

func (s *RuntimeSupervisor) updateExistingScroll(runtimeScroll *domain.RuntimeScroll, artifact string, knownDigest string, registryCredentials []domain.RegistryCredential, restartIfRunning bool) (*domain.RuntimeScroll, error) {
	wasRunning := runtimeScroll.Status == domain.RuntimeScrollStatusRunning
	existingRouting := make([]domain.RuntimeRouteAssignment, len(runtimeScroll.Routing))
	copy(existingRouting, runtimeScroll.Routing)

	s.mu.Lock()
	session := s.sessions[runtimeScroll.ID]
	delete(s.sessions, runtimeScroll.ID)
	s.mu.Unlock()
	if session != nil {
		session.Shutdown()
	}

	if wasRunning {
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
		runtimeScroll.Status = domain.RuntimeScrollStatusError
		runtimeScroll.LastError = err.Error()
		_ = s.store.UpdateScroll(runtimeScroll)
		return nil, err
	}
	if err := scroll.Validate(false); err != nil {
		runtimeScroll.Status = domain.RuntimeScrollStatusError
		runtimeScroll.LastError = err.Error()
		_ = s.store.UpdateScroll(runtimeScroll)
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
	runtimeScroll.Routing = preserveRoutingAssignments(existingRouting, scroll.Ports)
	runtimeScroll.LastError = ""
	if wasRunning || runtimeScroll.Status == domain.RuntimeScrollStatusStopped {
		runtimeScroll.Status = domain.RuntimeScrollStatusStopped
	} else {
		runtimeScroll.Status = domain.RuntimeScrollStatusCreated
	}
	if err := s.store.UpdateScroll(runtimeScroll); err != nil {
		return nil, err
	}
	if wasRunning && restartIfRunning {
		return s.StartScroll(runtimeScroll.ID)
	}
	return s.store.GetScroll(runtimeScroll.ID)
}

func preserveRoutingAssignments(assignments []domain.RuntimeRouteAssignment, ports []domain.Port) []domain.RuntimeRouteAssignment {
	if len(assignments) == 0 {
		return nil
	}
	names := map[string]struct{}{}
	for _, port := range ports {
		names[port.Name] = struct{}{}
	}
	kept := make([]domain.RuntimeRouteAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		if _, ok := names[assignment.PortName]; ok {
			kept = append(kept, assignment)
			continue
		}
		logger.Log().Warn("Dropping stale routing assignment after runtime update",
			zap.String("assignment", assignment.Name),
			zap.String("port_name", assignment.PortName),
		)
	}
	return kept
}
