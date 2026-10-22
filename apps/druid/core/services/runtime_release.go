package services

import (
	"context"
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

// InstalledRelease serves the descriptor reported by the last successful pull.
// The operation lock serializes queries with update/restore transitions.
func (s *RuntimeSupervisor) InstalledRelease(ctx context.Context, id string) (map[string]string, error) {
	unlock := s.lockRuntimeOperation(id)
	defer unlock()
	runtime, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	return s.installedRelease(ctx, runtime)
}

func (s *RuntimeSupervisor) installedRelease(ctx context.Context, runtime *domain.RuntimeScroll) (map[string]string, error) {
	switch runtime.InstalledReleaseState {
	case domain.InstalledReleaseReady:
		return installedReleaseIdentity(runtime.Root, runtime.ScrollYAML, runtime.ArtifactDigest)
	case "": // Older records need one read of the actual installed descriptor.
	default:
		// A lost callback may leave a worker still replacing files. Do not
		// inspect concurrently or advertise the previous cached release.
		return nil, fmt.Errorf("installed release is unconfirmed after an incomplete materialization")
	}
	installed, err := s.runPullWorker(ctx, s.runtimeBackend, ports.RuntimeWorkerModeInspect, runtime.ID, runtime.Artifact, runtime.Root, nil, "")
	if err != nil {
		return nil, err
	}
	identity, err := installedReleaseIdentity(runtime.Root, string(installed.ScrollYAML), installed.ArtifactDigest)
	if err != nil {
		return nil, err
	}
	// Keep the live session in sync so subsequent status writes cannot replace
	// the newly confirmed identity with its old copy.
	s.mu.Lock()
	session := s.sessions[runtime.ID]
	s.mu.Unlock()
	current := runtime
	if session != nil {
		session.mu.Lock()
		defer session.mu.Unlock()
		current = session.runtimeScroll
	}
	updated := *current
	updated.ArtifactDigest = installed.ArtifactDigest
	updated.ScrollYAML = string(installed.ScrollYAML)
	updated.InstalledReleaseState = domain.InstalledReleaseReady
	scrollService, err := coreservices.NewCachedScrollServiceWithPorts(updated.Root, installed.ScrollYAML, updated.ReservedPorts)
	if err != nil {
		return nil, err
	}
	updated.ScrollName = scrollService.GetCurrent().Name
	if err := s.store.UpdateScroll(&updated); err != nil {
		return nil, err
	}
	*runtime = updated
	if session != nil {
		*session.runtimeScroll = updated
		session.scrollService = scrollService
	}
	return identity, nil
}

func installedReleaseIdentity(root, yaml, digest string) (map[string]string, error) {
	scroll, err := domain.NewScrollFromBytes(root, []byte(yaml))
	if err != nil {
		return nil, err
	}
	if !acceptedUpdateReference.MatchString(scroll.Name + "@" + digest) {
		return nil, fmt.Errorf("invalid installed release identity")
	}
	return map[string]string{"repository": scroll.Name, "digest": digest}, nil
}
