package services

import (
	"context"
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

// InstalledRelease reads the actual runtime volume under the same operation
// lock as update/restore, so it cannot observe a partially replaced root.
func (s *RuntimeSupervisor) InstalledRelease(ctx context.Context, id string) (map[string]string, error) {
	unlock := s.lockRuntimeOperation(id)
	defer unlock()
	runtime, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	installed, err := s.runPullWorker(ctx, s.runtimeBackend, ports.RuntimeWorkerModeInspect, id, runtime.Artifact, runtime.Root, nil, "")
	if err != nil {
		return nil, err
	}
	scroll, err := domain.NewScrollFromBytes(runtime.Root, installed.ScrollYAML)
	if err != nil {
		return nil, err
	}
	if !acceptedUpdateReference.MatchString(scroll.Name + "@" + installed.ArtifactDigest) {
		return nil, fmt.Errorf("invalid installed release identity")
	}
	return map[string]string{"repository": scroll.Name, "digest": installed.ArtifactDigest}, nil
}
