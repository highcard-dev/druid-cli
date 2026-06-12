package services

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (s *RuntimeSupervisor) materializeNewScroll(ctx context.Context, runtimeService ports.RuntimeBackendInterface, artifact string, runtimeID string, namespace string, registryCredentials []domain.RegistryCredential) (*ports.RuntimeMaterialization, error) {
	storage := resolveArtifactMinDisk(artifact, registryCredentials)
	return s.runPullWorker(ctx, runtimeService, ports.RuntimeWorkerModeCreate, runtimeID, artifact, runtimeService.RootRef(runtimeID, namespace), registryCredentials, storage)
}

func (s *RuntimeSupervisor) runPullWorker(ctx context.Context, runtimeService ports.RuntimeBackendInterface, mode ports.RuntimeWorkerMode, runtimeID string, artifact string, root string, registryCredentials []domain.RegistryCredential, storage string) (*ports.RuntimeMaterialization, error) {
	if s.workerCallbacks == nil || s.workerCallbackURL == "" {
		return nil, fmt.Errorf("daemon materialization requires --worker-callback-url and --worker-callback-listen")
	}
	token, resultCh, err := s.workerCallbacks.Register(runtimeID)
	if err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, s.workerTimeout)
	defer cancel()
	callbackURL := s.workerCallbackURL + "/internal/v1/workers/" + runtimeID + "/complete"
	action := ports.RuntimeWorkerAction{
		Mode:                mode,
		RuntimeID:           runtimeID,
		Artifact:            artifact,
		Storage:             storage,
		RootRef:             root,
		MountPath:           "/scroll",
		CallbackURL:         callbackURL,
		CallbackToken:       token,
		RegistryCredentials: registryCredentials,
	}
	if err := runtimeService.SpawnPullWorker(waitCtx, action); err != nil {
		s.workerCallbacks.Cancel(runtimeID)
		return nil, err
	}
	select {
	case result, ok := <-resultCh:
		if !ok {
			return nil, fmt.Errorf("worker callback closed before result")
		}
		if result.Error != "" {
			return nil, errors.New(result.Error)
		}
		return &ports.RuntimeMaterialization{
			Artifact:       artifact,
			ArtifactDigest: result.ArtifactDigest,
			Root:           root,
			ScrollYAML:     []byte(result.ScrollYAML),
		}, nil
	case <-waitCtx.Done():
		s.workerCallbacks.Cancel(runtimeID)
		return nil, fmt.Errorf("worker action for runtime %s timed out: %w", runtimeID, waitCtx.Err())
	}
}

func resolveArtifactMinDisk(artifact string, registryCredentials []domain.RegistryCredential) string {
	if artifact == "" {
		return ""
	}
	if _, err := os.Stat(artifact); err == nil {
		return ""
	}
	oci := registry.NewOciClient(registry.NewCredentialStore(registryCredentials))
	info, err := oci.ResolveAnnotationInfo(artifact)
	if err != nil {
		logger.Log().Warn("Unable to resolve artifact min disk", zap.String("artifact", artifact), zap.Error(err))
		return ""
	}
	return info.MinDisk
}

func resolveArtifactDigest(artifact string, registryCredentials []domain.RegistryCredential) string {
	if artifact == "" {
		return ""
	}
	if _, err := os.Stat(artifact); err == nil {
		return ""
	}
	oci := registry.NewOciClient(registry.NewCredentialStore(registryCredentials))
	digest, err := oci.ResolveDigest(artifact)
	if err != nil {
		logger.Log().Warn("Unable to resolve artifact digest", zap.String("artifact", artifact), zap.Error(err))
		return ""
	}
	return digest
}
