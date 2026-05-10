package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	runtimebackend "github.com/highcard-dev/daemon/internal/runtime"
	runtimekubernetes "github.com/highcard-dev/daemon/internal/runtime/kubernetes"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

var ErrRuntimeMaterializationUnsupported = errors.New("runtime backend does not support daemon materialization")
var ErrRuntimeOperationUnsupported = errors.New("runtime backend does not support this operation")

var newKubernetesRuntimeStore = func(config runtimekubernetes.Config) (coreservices.RuntimeScrollStore, error) {
	return runtimekubernetes.NewConfigMapStateStore(config)
}

func NewRuntimeStore(stateDir string) (coreservices.RuntimeScrollStore, error) {
	if stateDir == "" {
		defaultStateDir, err := utils.DefaultRuntimeStateDir()
		if err != nil {
			return nil, err
		}
		stateDir = defaultStateDir
	}
	return coreservices.NewRuntimeStateStore(stateDir), nil
}

func NewRuntimeStoreForBackend(stateDir string, runtimeBackend string, kubernetesConfig runtimekubernetes.Config) (coreservices.RuntimeScrollStore, error) {
	if runtimeBackend == "kubernetes" {
		return newKubernetesRuntimeStore(kubernetesConfig)
	}
	return NewRuntimeStore(stateDir)
}

func LoadRuntimeScroll(stateDir string, id string) (coreservices.RuntimeScrollStore, *domain.RuntimeScroll, error) {
	store, err := NewRuntimeStore(stateDir)
	if err != nil {
		return nil, nil, err
	}
	runtimeScroll, err := store.GetScroll(id)
	if err != nil {
		if errors.Is(err, coreservices.ErrScrollNotFound) {
			return nil, nil, fmt.Errorf("runtime scroll %s not found", id)
		}
		return nil, nil, err
	}
	return store, runtimeScroll, nil
}

type RuntimeSupervisor struct {
	store          coreservices.RuntimeScrollStore
	manager        *coreservices.RuntimeScrollManager
	consoleService *coreservices.ConsoleManager
	runtimeBackend string
	runtimeOptions runtimebackend.Options

	mu       sync.Mutex
	sessions map[string]*RuntimeSession
}

func NewRuntimeSupervisor(
	store coreservices.RuntimeScrollStore,
	manager *coreservices.RuntimeScrollManager,
	consoleService *coreservices.ConsoleManager,
	runtimeBackend string,
	options ...runtimebackend.Option,
) *RuntimeSupervisor {
	runtimeOptions := runtimebackend.Options{}
	for _, option := range options {
		option(&runtimeOptions)
	}
	return &RuntimeSupervisor{
		store:          store,
		manager:        manager,
		consoleService: consoleService,
		runtimeBackend: runtimeBackend,
		runtimeOptions: runtimeOptions,
		sessions:       map[string]*RuntimeSession{},
	}
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

func (s *RuntimeSupervisor) Create(artifact string, name string, scrollRoot string, dataRoot string, start bool) (*domain.RuntimeScroll, error) {
	return s.create(artifact, name, scrollRoot, dataRoot, start)
}

func (s *RuntimeSupervisor) create(artifact string, name string, scrollRoot string, dataRoot string, autoStart bool) (*domain.RuntimeScroll, error) {
	runtimeService, err := runtimebackend.NewBackend(s.runtimeBackend, s.consoleService, runtimebackend.WithKubernetesConfig(s.runtimeOptions.Kubernetes))
	if err != nil {
		return nil, err
	}
	var scrollYAML []byte
	if scrollRoot == "" && dataRoot == "" {
		materializer, ok := runtimeService.(ports.RuntimeMaterializerInterface)
		if !ok {
			return nil, ErrRuntimeMaterializationUnsupported
		}
		materialized, err := materializer.MaterializeScroll(context.Background(), artifact, name)
		if err != nil {
			return nil, err
		}
		if materialized.Artifact != "" {
			artifact = materialized.Artifact
		}
		scrollRoot = materialized.ScrollRoot
		dataRoot = materialized.DataRoot
		scrollYAML = materialized.ScrollYAML
	} else {
		scrollYAML, err = runtimeService.ReadScrollFile(scrollRoot)
		if err != nil {
			return nil, err
		}
	}
	runtimeScroll, err := s.manager.Create(artifact, name, scrollRoot, dataRoot, scrollYAML)
	if err != nil {
		return nil, err
	}
	session, err := s.startSession(runtimeScroll)
	if err != nil {
		runtimeScroll.Status = domain.RuntimeScrollStatusError
		runtimeScroll.LastError = err.Error()
		_ = s.store.UpdateScroll(runtimeScroll)
		return nil, err
	}
	if autoStart {
		if err := session.AutoStartServe(); err != nil {
			runtimeScroll.Status = domain.RuntimeScrollStatusError
			runtimeScroll.LastError = err.Error()
			_ = s.store.UpdateScroll(runtimeScroll)
			return nil, err
		}
	}
	return runtimeScroll, nil
}

func (s *RuntimeSupervisor) Ensure(artifact string, name string, scrollRoot string, dataRoot string, start bool) (*domain.RuntimeScroll, error) {
	id := coreservices.RuntimeScrollIDFromName(name)
	if id != "" {
		runtimeScroll, err := s.store.GetScroll(id)
		if err == nil {
			if start {
				return s.StartScroll(runtimeScroll.ID)
			}
			return runtimeScroll, nil
		}
		if !errors.Is(err, coreservices.ErrScrollNotFound) {
			return nil, err
		}
	}
	runtimeScroll, err := s.create(artifact, name, scrollRoot, dataRoot, start)
	if err != nil {
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

func (s *RuntimeSupervisor) Delete(id string) error {
	return s.DeleteWithPolicy(id, false)
}

func (s *RuntimeSupervisor) DeleteWithPolicy(id string, purgeData bool) error {
	s.mu.Lock()
	session := s.sessions[id]
	delete(s.sessions, id)
	s.mu.Unlock()
	if session == nil {
		var err error
		session, err = s.sessionFor(id)
		if err != nil {
			return err
		}
		s.mu.Lock()
		delete(s.sessions, id)
		s.mu.Unlock()
	}
	if err := session.DeleteRuntime(purgeData); err != nil {
		return err
	}
	session.Shutdown()
	return s.store.DeleteScroll(id)
}

func (s *RuntimeSupervisor) StartScroll(id string) (*domain.RuntimeScroll, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	if err := session.AutoStartServe(); err != nil {
		session.markError(err)
		return nil, err
	}
	session.mu.Lock()
	session.runtimeScroll.Status = deriveRuntimeScrollStatus(session.runtimeScroll.Commands, session.scrollService.GetFile().Commands)
	if session.runtimeScroll.Status == domain.RuntimeScrollStatusCreated {
		session.runtimeScroll.Status = domain.RuntimeScrollStatusRunning
	}
	session.runtimeScroll.LastError = ""
	err = s.store.UpdateScroll(session.runtimeScroll)
	id = session.runtimeScroll.ID
	session.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSupervisor) Stop(id string) (*domain.RuntimeScroll, error) {
	s.mu.Lock()
	session := s.sessions[id]
	delete(s.sessions, id)
	s.mu.Unlock()
	if session == nil {
		var err error
		session, err = s.sessionFor(id)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		delete(s.sessions, id)
		s.mu.Unlock()
	}
	if err := session.StopRuntime(); err != nil {
		session.markError(err)
		return nil, err
	}
	session.Shutdown()
	return s.store.GetScroll(id)
}

func (s *RuntimeSupervisor) Run(id string, command string) (*domain.RuntimeScroll, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	return session.Run(command)
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

func (s *RuntimeSupervisor) Backup(id string, artifact string) (*domain.RuntimeScroll, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	if err := session.Backup(context.Background(), artifact); err != nil {
		session.markError(err)
		return nil, err
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSupervisor) Restore(id string, artifact string, restart bool) (*domain.RuntimeScroll, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	if err := session.Restore(context.Background(), artifact); err != nil {
		session.markError(err)
		return nil, err
	}
	if restart {
		return s.StartScroll(id)
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSupervisor) DataFile(id string, relativePath string) ([]byte, error) {
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	runtimeService, err := runtimebackend.NewBackend(s.runtimeBackend, s.consoleService, runtimebackend.WithKubernetesConfig(s.runtimeOptions.Kubernetes))
	if err != nil {
		return nil, err
	}
	fileBackend, ok := runtimeService.(ports.RuntimeFileBackendInterface)
	if !ok {
		return nil, ErrRuntimeOperationUnsupported
	}
	return fileBackend.ReadDataFile(context.Background(), runtimeScroll.DataRoot, relativePath)
}

func (s *RuntimeSupervisor) WriteDataFile(id string, relativePath string, data []byte) error {
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return err
	}
	runtimeService, err := runtimebackend.NewBackend(s.runtimeBackend, s.consoleService, runtimebackend.WithKubernetesConfig(s.runtimeOptions.Kubernetes))
	if err != nil {
		return err
	}
	fileBackend, ok := runtimeService.(ports.RuntimeFileBackendInterface)
	if !ok {
		return ErrRuntimeOperationUnsupported
	}
	return fileBackend.WriteDataFile(context.Background(), runtimeScroll.DataRoot, relativePath, data)
}

func (s *RuntimeSupervisor) ScrollFile(id string) (*domain.File, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	return session.scrollService.GetFile(), nil
}

func (s *RuntimeSupervisor) Queue(id string) (map[string]domain.ScrollLockStatus, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	return session.queueManager.GetQueue(), nil
}

func (s *RuntimeSupervisor) Procedures(id string) (map[string]domain.ScrollLockStatus, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	return session.queueManager.GetQueue(), nil
}

func (s *RuntimeSupervisor) sessionFor(id string) (*RuntimeSession, error) {
	s.mu.Lock()
	session := s.sessions[id]
	s.mu.Unlock()
	if session != nil {
		return session, nil
	}
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	return s.startSession(runtimeScroll)
}

func (s *RuntimeSupervisor) startSession(runtimeScroll *domain.RuntimeScroll) (*RuntimeSession, error) {
	s.mu.Lock()
	if session := s.sessions[runtimeScroll.ID]; session != nil {
		s.mu.Unlock()
		return session, nil
	}
	s.mu.Unlock()

	session, err := NewRuntimeSession(s.store, runtimeScroll, s.consoleService, s.runtimeBackend, runtimebackend.WithKubernetesConfig(s.runtimeOptions.Kubernetes))
	if err != nil {
		return nil, err
	}
	session.Start()

	s.mu.Lock()
	if existing := s.sessions[runtimeScroll.ID]; existing != nil {
		s.mu.Unlock()
		session.Shutdown()
		return existing, nil
	}
	s.sessions[runtimeScroll.ID] = session
	s.mu.Unlock()
	return session, nil
}

func (s *RuntimeSupervisor) markScrollError(runtimeScroll *domain.RuntimeScroll, err error) {
	logger.Log().Error("failed to restore runtime scroll", zap.String("scroll", runtimeScroll.ID), zap.Error(err))
	runtimeScroll.Status = domain.RuntimeScrollStatusError
	runtimeScroll.LastError = err.Error()
	if runtimeScroll.Commands == nil {
		runtimeScroll.Commands = map[string]domain.LockStatus{}
	}
	_ = s.store.UpdateScroll(runtimeScroll)
}

type RuntimeSession struct {
	store          coreservices.RuntimeScrollStore
	runtimeScroll  *domain.RuntimeScroll
	scrollService  *coreservices.ScrollService
	queueManager   *coreservices.QueueManager
	runtimeBackend ports.RuntimeBackendInterface

	mu      sync.Mutex
	started bool
}

func NewRuntimeSession(
	store coreservices.RuntimeScrollStore,
	runtimeScroll *domain.RuntimeScroll,
	consoleService *coreservices.ConsoleManager,
	runtimeBackend string,
	options ...runtimebackend.Option,
) (*RuntimeSession, error) {
	runtimeService, err := runtimebackend.NewBackend(runtimeBackend, consoleService, options...)
	if err != nil {
		return nil, err
	}
	if runtimeScroll.DataRoot == "" {
		return nil, fmt.Errorf("runtime scroll %s has no data root", runtimeScroll.ID)
	}
	scrollYAML := []byte(runtimeScroll.ScrollYAML)
	if len(scrollYAML) == 0 {
		scrollYAML, err = runtimeService.ReadScrollFile(runtimeScroll.ScrollRoot)
		if err != nil {
			return nil, err
		}
		runtimeScroll.ScrollYAML = string(scrollYAML)
		if err := store.UpdateScroll(runtimeScroll); err != nil {
			return nil, err
		}
	}
	scrollService, err := coreservices.NewCachedScrollService(runtimeScroll.ScrollRoot, scrollYAML)
	if err != nil {
		return nil, err
	}
	session := &RuntimeSession{
		store:          store,
		runtimeScroll:  runtimeScroll,
		scrollService:  scrollService,
		runtimeBackend: runtimeService,
	}
	processLauncher, err := coreservices.NewProcedureLauncherForRuntime(scrollService, runtimeService, runtimeScroll.DataRoot, runtimeScroll.ID, runtimeScroll.ScrollName, func() []domain.RuntimeRouteAssignment {
		session.mu.Lock()
		defer session.mu.Unlock()
		routing := make([]domain.RuntimeRouteAssignment, len(session.runtimeScroll.Routing))
		copy(routing, session.runtimeScroll.Routing)
		return routing
	})
	if err != nil {
		return nil, err
	}
	queueManager := coreservices.NewQueueManager(scrollService, processLauncher)
	session.queueManager = queueManager
	queueManager.SetStatusObserver(session.persistCommandStatus)
	return session, nil
}

func (s *RuntimeSession) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	s.started = true
	go s.queueManager.Work()
}

func (s *RuntimeSession) Hydrate() error {
	s.mu.Lock()
	statuses := copyCommandStatuses(s.runtimeScroll.Commands)
	runtimeStatus := s.runtimeScroll.Status
	s.mu.Unlock()
	commands := s.scrollService.GetFile().Commands
	if len(statuses) > 0 {
		filtered := map[string]domain.LockStatus{}
		removedStaleStatus := false
		for commandName, status := range statuses {
			command := commands[commandName]
			if command == nil {
				removedStaleStatus = true
				continue
			}
			// Kubernetes keeps persistent workloads alive; do not requeue them just because
			// the singleton API process restarted.
			if runtimeStatus == domain.RuntimeScrollStatusRunning && status.Status == domain.ScrollLockStatusDone && command.Run == domain.RunModePersistent {
				continue
			}
			filtered[commandName] = status
		}
		if removedStaleStatus {
			s.mu.Lock()
			for commandName := range s.runtimeScroll.Commands {
				if commands[commandName] == nil {
					delete(s.runtimeScroll.Commands, commandName)
				}
			}
			err := s.store.UpdateScroll(s.runtimeScroll)
			s.mu.Unlock()
			if err != nil {
				return err
			}
		}
		statuses = filtered
		if err := s.queueManager.HydrateCommandStatuses(statuses); err != nil {
			return err
		}
	}
	if err := s.AutoStartServe(); err != nil {
		return err
	}
	s.mu.Lock()
	s.runtimeScroll.Status = deriveRuntimeScrollStatus(s.runtimeScroll.Commands, s.scrollService.GetFile().Commands)
	err := s.store.UpdateScroll(s.runtimeScroll)
	s.mu.Unlock()
	return err
}

func (s *RuntimeSession) AutoStartServe() error {
	serveCommand := s.scrollService.GetFile().Serve
	if serveCommand == "" {
		return nil
	}
	if err := WriteRuntimeConfig(s.runtimeScroll, s.scrollService.GetFile(), s.runtimeBackend.Name()); err != nil {
		return err
	}
	if command := s.scrollService.GetFile().Commands[serveCommand]; command != nil && command.Run == domain.RunModePersistent {
		s.mu.Lock()
		status, ok := s.runtimeScroll.Commands[serveCommand]
		runtimeStatus := s.runtimeScroll.Status
		s.mu.Unlock()
		if ok && status.Status == domain.ScrollLockStatusDone && runtimeStatus == domain.RuntimeScrollStatusRunning {
			return nil
		}
	}
	if err := s.queueManager.AddForcedItem(serveCommand); err != nil && !errors.Is(err, coreservices.ErrAlreadyInQueue) {
		return err
	}
	return nil
}

func (s *RuntimeSession) Run(command string) (*domain.RuntimeScroll, error) {
	if err := WriteRuntimeConfig(s.runtimeScroll, s.scrollService.GetFile(), s.runtimeBackend.Name()); err != nil {
		return nil, err
	}
	s.refreshCommandState()
	targetCommand, err := s.scrollService.GetCommand(command)
	if err != nil {
		s.markError(err)
		return nil, err
	}
	longRunning := targetCommand.Run == domain.RunModeRestart || targetCommand.Run == domain.RunModePersistent
	s.rememberDoneDependencies(targetCommand, map[string]bool{})

	if err := s.queueManager.AddTempItem(command); err != nil {
		s.markError(err)
		return nil, err
	}
	if !longRunning {
		s.queueManager.WaitUntilEmpty()
	}

	s.mu.Lock()
	s.runtimeScroll.Status = deriveRuntimeScrollStatus(s.runtimeScroll.Commands, s.scrollService.GetFile().Commands)
	err = s.store.UpdateScroll(s.runtimeScroll)
	id := s.runtimeScroll.ID
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSession) refreshCommandState() {
	fresh, err := s.store.GetScroll(s.runtimeScroll.ID)
	if err != nil {
		return
	}
	commands := s.scrollService.GetFile().Commands
	removedStaleStatus := false
	for commandName := range fresh.Commands {
		if commands[commandName] == nil {
			delete(fresh.Commands, commandName)
			removedStaleStatus = true
		}
	}
	if removedStaleStatus {
		_ = s.store.UpdateScroll(fresh)
	}
	s.mu.Lock()
	s.runtimeScroll.Commands = copyCommandStatuses(fresh.Commands)
	s.runtimeScroll.Status = fresh.Status
	s.mu.Unlock()
}

func (s *RuntimeSession) rememberDoneDependencies(command *domain.CommandInstructionSet, seen map[string]bool) {
	if command == nil {
		return
	}
	for _, dependency := range command.Needs {
		if seen[dependency] {
			continue
		}
		seen[dependency] = true
		status, ok := s.runtimeScroll.Commands[dependency]
		if ok && status.Status == domain.ScrollLockStatusDone {
			s.queueManager.RememberDoneItem(dependency)
		}
		dependencyCommand, err := s.scrollService.GetCommand(dependency)
		if err == nil {
			s.rememberDoneDependencies(dependencyCommand, seen)
		}
	}
}

func (s *RuntimeSession) Ports() ([]domain.RuntimePortStatus, error) {
	s.mu.Lock()
	runtimeScroll := *s.runtimeScroll
	s.mu.Unlock()
	return s.runtimeBackend.ExpectedPorts(runtimeScroll.DataRoot, s.scrollService.GetFile().Commands, s.scrollService.GetFile().Ports)
}

func (s *RuntimeSession) RoutingTargets() ([]domain.RuntimeRoutingTarget, error) {
	routingBackend, ok := s.runtimeBackend.(ports.RuntimeRoutingBackendInterface)
	if !ok {
		return nil, ErrRuntimeOperationUnsupported
	}
	s.mu.Lock()
	runtimeScroll := *s.runtimeScroll
	s.mu.Unlock()
	return routingBackend.RoutingTargets(runtimeScroll.DataRoot, s.scrollService.GetFile().Commands, s.scrollService.GetFile().Ports)
}

func (s *RuntimeSession) ApplyRouting(assignments []domain.RuntimeRouteAssignment) (*domain.RuntimeScroll, error) {
	s.mu.Lock()
	s.runtimeScroll.Routing = assignments
	s.runtimeScroll.LastError = ""
	err := s.store.UpdateScroll(s.runtimeScroll)
	id := s.runtimeScroll.ID
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSession) StopRuntime() error {
	lifecycleBackend, ok := s.runtimeBackend.(ports.RuntimeLifecycleBackendInterface)
	if !ok {
		return ErrRuntimeOperationUnsupported
	}
	s.mu.Lock()
	dataRoot := s.runtimeScroll.DataRoot
	s.mu.Unlock()
	if err := lifecycleBackend.StopRuntime(dataRoot); err != nil {
		return err
	}
	s.mu.Lock()
	s.runtimeScroll.Status = domain.RuntimeScrollStatusStopped
	s.runtimeScroll.LastError = ""
	err := s.store.UpdateScroll(s.runtimeScroll)
	s.mu.Unlock()
	return err
}

func (s *RuntimeSession) DeleteRuntime(purgeData bool) error {
	lifecycleBackend, ok := s.runtimeBackend.(ports.RuntimeLifecycleBackendInterface)
	if !ok {
		return ErrRuntimeOperationUnsupported
	}
	s.mu.Lock()
	dataRoot := s.runtimeScroll.DataRoot
	s.mu.Unlock()
	return lifecycleBackend.DeleteRuntime(dataRoot, purgeData)
}

func (s *RuntimeSession) Backup(ctx context.Context, artifact string) error {
	backupBackend, ok := s.runtimeBackend.(ports.RuntimeBackupBackendInterface)
	if !ok {
		return ErrRuntimeOperationUnsupported
	}
	s.mu.Lock()
	dataRoot := s.runtimeScroll.DataRoot
	s.mu.Unlock()
	return backupBackend.BackupRuntime(ctx, dataRoot, artifact)
}

func (s *RuntimeSession) Restore(ctx context.Context, artifact string) error {
	backupBackend, ok := s.runtimeBackend.(ports.RuntimeBackupBackendInterface)
	if !ok {
		return ErrRuntimeOperationUnsupported
	}
	s.mu.Lock()
	dataRoot := s.runtimeScroll.DataRoot
	s.mu.Unlock()
	if err := backupBackend.RestoreRuntime(ctx, dataRoot, artifact); err != nil {
		return err
	}
	scrollYAML, err := s.runtimeBackend.ReadScrollFile(dataRoot)
	if err != nil {
		return err
	}
	scrollService, err := coreservices.NewCachedScrollService(dataRoot, scrollYAML)
	if err != nil {
		return err
	}
	processLauncher, err := coreservices.NewProcedureLauncherForRuntime(scrollService, s.runtimeBackend, dataRoot, s.runtimeScroll.ID, s.runtimeScroll.ScrollName, func() []domain.RuntimeRouteAssignment {
		s.mu.Lock()
		defer s.mu.Unlock()
		routing := make([]domain.RuntimeRouteAssignment, len(s.runtimeScroll.Routing))
		copy(routing, s.runtimeScroll.Routing)
		return routing
	})
	if err != nil {
		return err
	}
	queueManager := coreservices.NewQueueManager(scrollService, processLauncher)
	queueManager.SetStatusObserver(s.persistCommandStatus)

	s.mu.Lock()
	commands := scrollService.GetFile().Commands
	for commandName := range s.runtimeScroll.Commands {
		if commands[commandName] == nil {
			delete(s.runtimeScroll.Commands, commandName)
		}
	}
	s.runtimeScroll.Artifact = artifact
	s.runtimeScroll.ScrollRoot = dataRoot
	s.runtimeScroll.ScrollYAML = string(scrollYAML)
	s.runtimeScroll.Status = domain.RuntimeScrollStatusStopped
	s.runtimeScroll.LastError = ""
	s.scrollService = scrollService
	s.queueManager = queueManager
	if s.started {
		go queueManager.Work()
	}
	err = s.store.UpdateScroll(s.runtimeScroll)
	s.mu.Unlock()
	return err
}

func (s *RuntimeSession) Shutdown() {
	s.queueManager.Shutdown()
}

func (s *RuntimeSession) persistCommandStatus(command string, status domain.ScrollLockStatus, exitCode *int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	commands := s.scrollService.GetFile().Commands
	if commands[command] == nil {
		return
	}
	if s.runtimeScroll.Commands == nil {
		s.runtimeScroll.Commands = map[string]domain.LockStatus{}
	}
	for commandName := range s.runtimeScroll.Commands {
		if commands[commandName] == nil {
			delete(s.runtimeScroll.Commands, commandName)
		}
	}
	s.runtimeScroll.Commands[command] = domain.LockStatus{
		Status:           status,
		ExitCode:         exitCode,
		LastStatusChange: time.Now().Unix(),
	}
	s.runtimeScroll.Status = deriveRuntimeScrollStatus(s.runtimeScroll.Commands, s.scrollService.GetFile().Commands)
	if err := s.store.UpdateScroll(s.runtimeScroll); err != nil {
		logger.Log().Error("failed to persist command status", zap.String("scroll", s.runtimeScroll.ID), zap.String("command", command), zap.Error(err))
	}
}

func (s *RuntimeSession) markError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeScroll.Status = domain.RuntimeScrollStatusError
	if err != nil {
		s.runtimeScroll.LastError = err.Error()
	}
	_ = s.store.UpdateScroll(s.runtimeScroll)
}

func WriteRuntimeConfig(runtimeScroll *domain.RuntimeScroll, scroll *domain.File, runtimeBackend string) error {
	if strings.HasPrefix(runtimeScroll.DataRoot, "k8s://") {
		return nil
	}
	configPath := filepath.Join(runtimeScroll.DataRoot, domain.RuntimeDataDir, domain.RuntimeConfigDir, domain.RuntimeConfigFile)
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	config := domain.RuntimeConfig{
		SchemaVersion: "druid.runtime/v1",
		Scroll: domain.RuntimeConfigScroll{
			ID:       runtimeScroll.ID,
			Name:     runtimeScroll.ScrollName,
			Artifact: runtimeScroll.Artifact,
		},
		Paths: domain.RuntimeConfigPaths{
			Data:          ".",
			RuntimeConfig: filepath.ToSlash(filepath.Join(domain.RuntimeConfigDir, domain.RuntimeConfigFile)),
		},
		Ports:         scroll.Ports,
		ExpectedPorts: runtimeExpectedPorts(scroll),
		Runtime: domain.RuntimeConfigRuntime{
			Backend:     runtimeBackend,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

func runtimeExpectedPorts(scroll *domain.File) []domain.RuntimeExpectedPort {
	portsByName := map[string]domain.Port{}
	for _, port := range scroll.Ports {
		portsByName[port.Name] = port
	}
	ports := []domain.RuntimeExpectedPort{}
	for commandName, command := range scroll.Commands {
		if command == nil {
			continue
		}
		for idx, procedure := range command.Procedures {
			if procedure == nil {
				continue
			}
			procedureName := fmt.Sprintf("%s.%d", commandName, idx)
			if procedure.Id != nil {
				procedureName = *procedure.Id
			}
			for _, expectedPort := range procedure.ExpectedPorts {
				port := portsByName[expectedPort.Name]
				ports = append(ports, domain.RuntimeExpectedPort{
					Name:             expectedPort.Name,
					Procedure:        procedureName,
					Port:             port.Port,
					Protocol:         port.Protocol,
					KeepAliveTraffic: expectedPort.KeepAliveTraffic,
				})
			}
		}
	}
	return ports
}

func deriveRuntimeScrollStatus(statuses map[string]domain.LockStatus, commands map[string]*domain.CommandInstructionSet) domain.RuntimeScrollStatus {
	if len(statuses) == 0 {
		return domain.RuntimeScrollStatusCreated
	}
	hasActive := false
	hasPersistentDone := false
	for commandName, status := range statuses {
		if status.Status == domain.ScrollLockStatusError {
			return domain.RuntimeScrollStatusError
		}
		if status.Status == domain.ScrollLockStatusRunning || status.Status == domain.ScrollLockStatusWaiting {
			hasActive = true
		}
		if status.Status == domain.ScrollLockStatusDone {
			if command := commands[commandName]; command != nil && command.Run == domain.RunModePersistent {
				hasPersistentDone = true
			}
		}
	}
	if hasActive || hasPersistentDone {
		return domain.RuntimeScrollStatusRunning
	}
	return domain.RuntimeScrollStatusStopped
}

func copyCommandStatuses(statuses map[string]domain.LockStatus) map[string]domain.LockStatus {
	copied := map[string]domain.LockStatus{}
	for command, status := range statuses {
		copied[command] = status
	}
	return copied
}
