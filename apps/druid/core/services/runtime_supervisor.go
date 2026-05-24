package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

// RuntimeSupervisor is the daemon-facing coordinator. It owns persisted runtime
// truth and session lifetimes; Docker/Kubernetes resource details stay behind
// the runtime backend.
type RuntimeSupervisor struct {
	store             ports.RuntimeScrollStore
	manager           *coreservices.RuntimeScrollManager
	runtimeBackend    ports.RuntimeBackendInterface
	workerCallbacks   *WorkerCallbackManager
	workerCallbackURL string
	workerDaemonURL   string
	internalToken     string
	authJWKSURL       string
	runtimeJWKSURL    string

	mu       sync.Mutex
	sessions map[string]*RuntimeSession
}

func NewRuntimeSupervisor(
	store ports.RuntimeScrollStore,
	manager *coreservices.RuntimeScrollManager,
	runtimeBackend ports.RuntimeBackendInterface,
) *RuntimeSupervisor {
	return &RuntimeSupervisor{
		store:          store,
		manager:        manager,
		runtimeBackend: runtimeBackend,
		sessions:       map[string]*RuntimeSession{},
	}
}

func (s *RuntimeSupervisor) SetWorkerCallbacks(callbacks *WorkerCallbackManager, callbackURL string) {
	s.workerCallbacks = callbacks
	s.workerCallbackURL = strings.TrimRight(callbackURL, "/")
}

func (s *RuntimeSupervisor) SetDevWorkerConfig(daemonURL string, internalToken string, authJWKSURL string, runtimeJWKSURL string) {
	s.workerDaemonURL = strings.TrimRight(daemonURL, "/")
	s.internalToken = internalToken
	s.authJWKSURL = authJWKSURL
	s.runtimeJWKSURL = runtimeJWKSURL
}

func (s *RuntimeSupervisor) Start() error {
	scrolls, err := s.store.ListScrolls()
	if err != nil {
		return err
	}
	for _, runtimeScroll := range scrolls {
		if runtimeScroll.Status == domain.RuntimeScrollStatusDeleted {
			continue
		}
		if runtimeScroll.Status != domain.RuntimeScrollStatusRunning {
			continue
		}
		session, err := s.startSession(runtimeScroll)
		if err != nil {
			s.markScrollError(runtimeScroll, err)
			continue
		}
		if err := session.Hydrate(); err != nil {
			s.markScrollError(runtimeScroll, err)
			continue
		}
	}
	return nil
}

func (s *RuntimeSupervisor) Create(artifact string, name string, registryCredentials []domain.RegistryCredential) (*domain.RuntimeScroll, error) {
	return s.CreateWithOwner(artifact, name, "", "", registryCredentials)
}

func (s *RuntimeSupervisor) CreateWithOwner(artifact string, name string, ownerID string, namespace string, registryCredentials []domain.RegistryCredential) (*domain.RuntimeScroll, error) {
	id := coreservices.RuntimeScrollIDFromName(name)
	if id == "" {
		id = uuid.NewString()
	}
	if _, err := s.store.GetScroll(id); err == nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrRuntimeScrollAlreadyExists, id)
	} else if !errors.Is(err, domain.ErrRuntimeScrollNotFound) {
		return nil, err
	}
	placeholder := &domain.RuntimeScroll{
		ID:       id,
		OwnerID:  ownerID,
		Artifact: artifact,
		Root:     s.runtimeBackend.RootRef(id, namespace),
		Status:   domain.RuntimeScrollStatusCreated,
		Commands: map[string]domain.LockStatus{},
	}
	if err := s.store.CreateScroll(placeholder); err != nil {
		return nil, err
	}
	markPlaceholderError := func(cause error) {
		placeholder.Status = domain.RuntimeScrollStatusError
		placeholder.LastError = cause.Error()
		_ = s.store.UpdateScroll(placeholder)
	}

	materialized, err := s.materializeNewScroll(context.Background(), s.runtimeBackend, artifact, id, namespace, registryCredentials)
	if err != nil {
		markPlaceholderError(err)
		return nil, err
	}
	if materialized.Artifact != "" {
		artifact = materialized.Artifact
	}
	placeholder, err = s.applyMaterializedScroll(placeholder, artifact, materialized)
	if err != nil {
		return nil, err
	}
	return placeholder, nil
}

func (s *RuntimeSupervisor) Ensure(artifact string, name string, registryCredentials []domain.RegistryCredential) (*domain.RuntimeScroll, error) {
	return s.EnsureWithOwner(artifact, name, "", "", registryCredentials)
}

func (s *RuntimeSupervisor) EnsureWithOwner(artifact string, name string, ownerID string, namespace string, registryCredentials []domain.RegistryCredential) (*domain.RuntimeScroll, error) {
	id := coreservices.RuntimeScrollIDFromName(name)
	if id != "" {
		runtimeScroll, err := s.store.GetScroll(id)
		if err == nil {
			if namespace != "" && runtimeScroll.Root != "" {
				expectedRoot := s.runtimeBackend.RootRef(id, namespace)
				if runtimeScroll.Root != expectedRoot {
					return nil, fmt.Errorf("runtime %s already uses root %s; requested namespace %s would use %s", id, runtimeScroll.Root, namespace, expectedRoot)
				}
			}
			if runtimeScroll.ScrollYAML == "" {
				if runtimeScroll.Status == domain.RuntimeScrollStatusError {
					if ownerID != "" && runtimeScroll.OwnerID != ownerID {
						runtimeScroll.OwnerID = ownerID
						if err := s.store.UpdateScroll(runtimeScroll); err != nil {
							return nil, err
						}
					}
					return runtimeScroll, nil
				}
				if artifact == "" {
					artifact = runtimeScroll.Artifact
				}
				materialized, err := s.materializeNewScroll(context.Background(), s.runtimeBackend, artifact, id, namespace, registryCredentials)
				if err != nil {
					runtimeScroll.Status = domain.RuntimeScrollStatusError
					runtimeScroll.LastError = err.Error()
					if ownerID != "" {
						runtimeScroll.OwnerID = ownerID
					}
					_ = s.store.UpdateScroll(runtimeScroll)
					return nil, err
				}
				if materialized.Artifact != "" {
					artifact = materialized.Artifact
				}
				if ownerID != "" {
					runtimeScroll.OwnerID = ownerID
				}
				return s.applyMaterializedScroll(runtimeScroll, artifact, materialized)
			}
			if runtimeScroll.Status == domain.RuntimeScrollStatusError && (artifact == "" || artifact == runtimeScroll.Artifact) {
				if ownerID != "" && runtimeScroll.OwnerID != ownerID {
					runtimeScroll.OwnerID = ownerID
					if err := s.store.UpdateScroll(runtimeScroll); err != nil {
						return nil, err
					}
				}
				return runtimeScroll, nil
			}
			if artifact != "" {
				nextDigest := resolveArtifactDigest(artifact, registryCredentials)
				artifactChanged := artifact != runtimeScroll.Artifact
				digestChanged := nextDigest != "" && nextDigest != runtimeScroll.ArtifactDigest
				if artifactChanged || digestChanged {
					updated, err := s.updateExistingScroll(runtimeScroll, artifact, nextDigest, registryCredentials, false)
					if err != nil {
						return nil, err
					}
					if ownerID != "" && updated.OwnerID != ownerID {
						updated.OwnerID = ownerID
						if err := s.store.UpdateScroll(updated); err != nil {
							return nil, err
						}
					}
					return updated, nil
				}
			}
			if ownerID != "" && runtimeScroll.OwnerID != ownerID {
				runtimeScroll.OwnerID = ownerID
				if err := s.store.UpdateScroll(runtimeScroll); err != nil {
					return nil, err
				}
			}
			return runtimeScroll, nil
		}
		if !errors.Is(err, domain.ErrRuntimeScrollNotFound) {
			return nil, err
		}
	}
	runtimeScroll, err := s.CreateWithOwner(artifact, name, ownerID, namespace, registryCredentials)
	if err != nil {
		return nil, err
	}
	return runtimeScroll, nil
}

func (s *RuntimeSupervisor) applyMaterializedScroll(runtimeScroll *domain.RuntimeScroll, artifact string, materialized *ports.RuntimeMaterialization) (*domain.RuntimeScroll, error) {
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
	runtimeScroll.Artifact = artifact
	runtimeScroll.ArtifactDigest = materialized.ArtifactDigest
	runtimeScroll.Root = materialized.Root
	runtimeScroll.ScrollName = scroll.Name
	runtimeScroll.ScrollYAML = string(materialized.ScrollYAML)
	runtimeScroll.Status = domain.RuntimeScrollStatusCreated
	runtimeScroll.LastError = ""
	runtimeScroll.Commands = map[string]domain.LockStatus{}
	if err := s.store.UpdateScroll(runtimeScroll); err != nil {
		return nil, err
	}
	return runtimeScroll, nil
}

func (s *RuntimeSupervisor) List() ([]*domain.RuntimeScroll, error) {
	return s.store.ListScrolls()
}

func (s *RuntimeSupervisor) Get(id string) (*domain.RuntimeScroll, error) {
	return s.store.GetScroll(id)
}
