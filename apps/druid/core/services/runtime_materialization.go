package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

var ErrRuntimeMaterializationUnsupported = errors.New("runtime backend does not support daemon materialization")

func (s *RuntimeSupervisor) materializeNewScroll(ctx context.Context, runtimeService ports.RuntimeBackendInterface, artifact string, name string, registryCredentials []domain.RegistryCredential) (*ports.RuntimeMaterialization, error) {
	id := coreservices.RuntimeScrollIDFromName(name)
	if id == "" {
		return nil, ErrRuntimeMaterializationUnsupported
	}
	return s.runPullWorker(ctx, runtimeService, ports.RuntimeWorkerModeCreate, id, artifact, s.store.Root(id), registryCredentials)
}

func (s *RuntimeSupervisor) runPullWorker(ctx context.Context, runtimeService ports.RuntimeBackendInterface, mode ports.RuntimeWorkerMode, runtimeID string, artifact string, root string, registryCredentials []domain.RegistryCredential) (*ports.RuntimeMaterialization, error) {
	token, resultCh, err := s.workerCallbacks.Register(runtimeID)
	if err != nil {
		return nil, err
	}
	callbackURL := s.workerCallbackURL + "/internal/v1/workers/" + runtimeID + "/complete"
	action := ports.RuntimeWorkerAction{
		Mode:                mode,
		RuntimeID:           runtimeID,
		Artifact:            artifact,
		RootRef:             root,
		MountPath:           "/scroll",
		CallbackURL:         callbackURL,
		CallbackToken:       token,
		RegistryCredentials: registryCredentials,
	}
	if err := runtimeService.SpawnPullWorker(ctx, action); err != nil {
		s.workerCallbacks.Cancel(runtimeID)
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
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
