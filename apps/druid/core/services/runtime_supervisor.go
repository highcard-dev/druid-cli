package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
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
	workerTimeout     time.Duration

	mu       sync.Mutex
	sessions map[string]*RuntimeSession
}

type EnsureOptions struct {
	Artifact            string
	Name                string
	OwnerID             string
	Namespace           string
	RegistryCredentials []domain.RegistryCredential
}

// developerPorts are deployment-owned platform ports. They are allocated when
// a runtime is created and never changed by commands, the SPA, or later ensure
// requests.
var developerPorts = []domain.Port{
	{Name: "webdav", Port: 8084, Protocol: "http"},
	{Name: "vscode", Port: 3333, Protocol: "http"},
	{Name: "ssh", Port: 2222, Protocol: "tcp"},
}

func fixedDeveloperPorts() []domain.Port {
	return append([]domain.Port(nil), developerPorts...)
}

func NewRuntimeSupervisor(
	store ports.RuntimeScrollStore,
	manager *coreservices.RuntimeScrollManager,
	backendFactory ports.RuntimeBackendFactory,
) (*RuntimeSupervisor, error) {
	if backendFactory == nil {
		return nil, fmt.Errorf("runtime backend factory is required")
	}
	supervisor := &RuntimeSupervisor{
		store:         store,
		manager:       manager,
		workerTimeout: 20 * time.Minute,
		sessions:      map[string]*RuntimeSession{},
	}
	runtimeBackend, err := backendFactory.Create(supervisor)
	if err != nil {
		return nil, err
	}
	if runtimeBackend == nil {
		return nil, fmt.Errorf("runtime backend factory returned nil")
	}
	supervisor.runtimeBackend = runtimeBackend
	return supervisor, nil
}

func (s *RuntimeSupervisor) ObserveProcedureStatus(update ports.ProcedureStatusUpdate) {
	s.mu.Lock()
	session := s.sessions[update.RuntimeID]
	s.mu.Unlock()
	if session == nil {
		logger.Log().Warn("Ignoring procedure status for an unknown runtime", zap.String("runtime", update.RuntimeID), zap.String("command", update.Command), zap.String("procedure", update.Procedure), zap.String("status", string(update.Status)))
		return
	}
	session.persistProcedureStatus(update.Command, update.Procedure, update.Status, update.ExitCode)
}

func (s *RuntimeSupervisor) SetWorkerCallbacks(callbacks *WorkerCallbackManager, callbackURL string) {
	s.workerCallbacks = callbacks
	s.workerCallbackURL = strings.TrimRight(callbackURL, "/")
}

func (s *RuntimeSupervisor) SetWorkerTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	s.workerTimeout = timeout
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
	return s.createWithOwner(artifact, name, ownerID, namespace, registryCredentials)
}

func (s *RuntimeSupervisor) createWithOwner(artifact string, name string, ownerID string, namespace string, registryCredentials []domain.RegistryCredential) (*domain.RuntimeScroll, error) {
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
		ID:            id,
		OwnerID:       ownerID,
		Artifact:      artifact,
		Root:          s.runtimeBackend.RootRef(id, namespace),
		Status:        domain.RuntimeScrollStatusCreated,
		Procedures:    domain.ProcedureStatusMap{},
		ReservedPorts: fixedDeveloperPorts(),
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

func (s *RuntimeSupervisor) Ensure(options EnsureOptions) (*domain.RuntimeScroll, error) {
	id := coreservices.RuntimeScrollIDFromName(options.Name)
	if id != "" {
		runtimeScroll, err := s.store.GetScroll(id)
		if err == nil {
			if options.Namespace != "" && runtimeScroll.Root != "" {
				expectedRoot := s.runtimeBackend.RootRef(id, options.Namespace)
				if runtimeScroll.Root != expectedRoot {
					return nil, fmt.Errorf("runtime %s already uses root %s; requested namespace %s would use %s", id, runtimeScroll.Root, options.Namespace, expectedRoot)
				}
			}
			if runtimeScroll.ScrollYAML == "" {
				applyEnsureOptions(runtimeScroll, options)
				artifact := options.Artifact
				if artifact == "" {
					artifact = runtimeScroll.Artifact
				}
				materialized, err := s.materializeNewScroll(context.Background(), s.runtimeBackend, artifact, id, options.Namespace, options.RegistryCredentials)
				if err != nil {
					runtimeScroll.Status = domain.RuntimeScrollStatusError
					runtimeScroll.LastError = err.Error()
					_ = s.store.UpdateScroll(runtimeScroll)
					return nil, err
				}
				if materialized.Artifact != "" {
					artifact = materialized.Artifact
				}
				return s.applyMaterializedScroll(runtimeScroll, artifact, materialized)
			}
			if runtimeScroll.Status == domain.RuntimeScrollStatusError && (options.Artifact == "" || options.Artifact == runtimeScroll.Artifact) {
				return s.persistEnsureOptions(runtimeScroll, options)
			}
			if options.Artifact != "" {
				nextDigest := resolveArtifactDigest(options.Artifact, options.RegistryCredentials)
				artifactChanged := options.Artifact != runtimeScroll.Artifact
				digestChanged := nextDigest != "" && nextDigest != runtimeScroll.ArtifactDigest
				if artifactChanged || digestChanged {
					applyEnsureOptions(runtimeScroll, options)
					return s.updateExistingScroll(runtimeScroll, options.Artifact, nextDigest, options.RegistryCredentials, false)
				}
			}
			return s.persistEnsureOptions(runtimeScroll, options)
		}
		if !errors.Is(err, domain.ErrRuntimeScrollNotFound) {
			return nil, err
		}
	}
	return s.createWithOwner(options.Artifact, options.Name, options.OwnerID, options.Namespace, options.RegistryCredentials)
}

func applyEnsureOptions(runtimeScroll *domain.RuntimeScroll, options EnsureOptions) bool {
	changed := false
	if options.OwnerID != "" && runtimeScroll.OwnerID != options.OwnerID {
		runtimeScroll.OwnerID = options.OwnerID
		changed = true
	}
	return changed
}

func (s *RuntimeSupervisor) persistEnsureOptions(runtimeScroll *domain.RuntimeScroll, options EnsureOptions) (*domain.RuntimeScroll, error) {
	s.mu.Lock()
	session := s.sessions[runtimeScroll.ID]
	s.mu.Unlock()
	if session != nil {
		session.mu.Lock()
		defer session.mu.Unlock()
		runtimeScroll = session.runtimeScroll
	}

	updated := *runtimeScroll
	if !applyEnsureOptions(&updated, options) {
		return runtimeScroll, nil
	}
	var scrollService *coreservices.ScrollService
	if updated.ScrollYAML != "" {
		var err error
		scrollService, err = coreservices.NewCachedScrollServiceWithPorts(updated.Root, []byte(updated.ScrollYAML), updated.ReservedPorts)
		if err != nil {
			return nil, err
		}
	}
	if err := s.store.UpdateScroll(&updated); err != nil {
		return nil, err
	}
	*runtimeScroll = updated
	if session != nil && scrollService != nil {
		session.scrollService = scrollService
	}
	return runtimeScroll, nil
}

func (s *RuntimeSupervisor) applyMaterializedScroll(runtimeScroll *domain.RuntimeScroll, artifact string, materialized *ports.RuntimeMaterialization) (*domain.RuntimeScroll, error) {
	scrollService, err := coreservices.NewCachedScrollServiceWithPorts(materialized.Root, materialized.ScrollYAML, runtimeScroll.ReservedPorts)
	if err != nil {
		runtimeScroll.Status = domain.RuntimeScrollStatusError
		runtimeScroll.LastError = err.Error()
		_ = s.store.UpdateScroll(runtimeScroll)
		return nil, err
	}
	scroll := scrollService.GetCurrent()
	runtimeScroll.Artifact = artifact
	runtimeScroll.ArtifactDigest = materialized.ArtifactDigest
	runtimeScroll.Root = materialized.Root
	runtimeScroll.ScrollName = scroll.Name
	runtimeScroll.ScrollYAML = string(materialized.ScrollYAML)
	runtimeScroll.Status = domain.RuntimeScrollStatusCreated
	runtimeScroll.LastError = ""
	runtimeScroll.Procedures = domain.ProcedureStatusMap{}
	if err := s.store.UpdateScroll(runtimeScroll); err != nil {
		return nil, err
	}
	if err := s.publishDeclaredPrivateUI(runtimeScroll, scroll); err != nil {
		return nil, err
	}
	return s.store.GetScroll(runtimeScroll.ID)
}

func (s *RuntimeSupervisor) publishDeclaredPrivateUI(runtimeScroll *domain.RuntimeScroll, scroll *domain.Scroll) error {
	if scroll.UI == nil || scroll.UI.Private == nil {
		return nil
	}
	_, err := s.PublishUIPackage(runtimeScroll.ID, string(domain.RuntimeUIPackageScopePrivate), scroll.UI.Private.Path)
	if err != nil {
		runtimeScroll.Status = domain.RuntimeScrollStatusError
		runtimeScroll.LastError = fmt.Sprintf("publish declared private UI: %v", err)
		_ = s.store.UpdateScroll(runtimeScroll)
		return fmt.Errorf("publish declared private UI: %w", err)
	}
	return nil
}

func (s *RuntimeSupervisor) List() ([]*domain.RuntimeScroll, error) {
	return s.store.ListScrolls()
}

func (s *RuntimeSupervisor) Get(id string) (*domain.RuntimeScroll, error) {
	return s.store.GetScroll(id)
}
