package services

import (
	"context"
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
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
	return s.installedRelease(runtime)
}

func (s *RuntimeSupervisor) installedRelease(runtime *domain.RuntimeScroll) (map[string]string, error) {
	if runtime.InstalledReleaseState != domain.InstalledReleaseReady {
		return nil, fmt.Errorf("installed release is unconfirmed; a successful pull is required")
	}
	return installedReleaseIdentity(runtime.Root, runtime.ScrollYAML, runtime.ArtifactDigest)
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
