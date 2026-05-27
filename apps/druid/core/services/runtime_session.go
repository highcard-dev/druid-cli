package services

import (
	"context"
	"fmt"
	"sync"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

// RuntimeSession is the live execution view for one persisted scroll. It owns
// the command queue and cached scroll.yaml; storage and containers stay behind
// the runtime backend.
type RuntimeSession struct {
	store             ports.RuntimeScrollStore
	runtimeScroll     *domain.RuntimeScroll
	scrollService     *coreservices.ScrollService
	queueManager      *coreservices.QueueManager
	watchService      ports.WatchServiceInterface
	runtimeBackend    ports.RuntimeBackendInterface
	procedures        ports.ProcedureLauchnerInterface
	devWatchPaths     []string
	devCommands       []string
	devDaemonURL      string
	devDaemonToken    string
	devAuthJWKSURL    string
	devRuntimeJWKSURL string
	devWatchBridge    *domain.BroadcastChannel
	devWatchCancel    context.CancelFunc

	mu      sync.Mutex
	started bool
}

func NewRuntimeSession(
	store ports.RuntimeScrollStore,
	runtimeScroll *domain.RuntimeScroll,
	runtimeService ports.RuntimeBackendInterface,
) (*RuntimeSession, error) {
	if runtimeScroll.Root == "" {
		return nil, fmt.Errorf("runtime scroll %s has no root", runtimeScroll.ID)
	}
	scrollYAML := []byte(runtimeScroll.ScrollYAML)
	if len(scrollYAML) == 0 {
		return nil, fmt.Errorf("runtime scroll %s has no scroll_yaml", runtimeScroll.ID)
	}
	scrollService, err := coreservices.NewCachedScrollService(runtimeScroll.Root, scrollYAML)
	if err != nil {
		return nil, err
	}
	session := &RuntimeSession{
		store:          store,
		runtimeScroll:  runtimeScroll,
		scrollService:  scrollService,
		runtimeBackend: runtimeService,
	}
	queueManager, processLauncher, err := session.newQueue(scrollService, runtimeScroll.Root, runtimeScroll.ScrollName)
	if err != nil {
		return nil, err
	}
	session.queueManager = queueManager
	session.procedures = processLauncher
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

func (s *RuntimeSession) Shutdown() {
	s.queueManager.Shutdown()
}

func (s *RuntimeSession) startQueue() {
	s.mu.Lock()
	queueManager := s.queueManager
	s.mu.Unlock()
	go queueManager.Work()
}

func (s *RuntimeSession) newQueue(scrollService *coreservices.ScrollService, root string, scrollName string) (*coreservices.QueueManager, *coreservices.ProcedureLauncher, error) {
	processLauncher, err := coreservices.NewProcedureLauncherForRuntime(scrollService, s.runtimeBackend, root, s.runtimeScroll.ID, scrollName, func() []domain.RuntimeRouteAssignment {
		s.mu.Lock()
		defer s.mu.Unlock()
		routing := make([]domain.RuntimeRouteAssignment, len(s.runtimeScroll.Routing))
		copy(routing, s.runtimeScroll.Routing)
		return routing
	})
	if err != nil {
		return nil, nil, err
	}
	queueManager := coreservices.NewQueueManager(scrollService, processLauncher)
	queueManager.SetStatusObserver(s.persistCommandStatus)
	return queueManager, processLauncher, nil
}

func (s *RuntimeSession) replaceQueue(start bool) (*coreservices.QueueManager, error) {
	s.mu.Lock()
	scrollService := s.scrollService
	root := s.runtimeScroll.Root
	scrollName := s.runtimeScroll.ScrollName
	oldQueue := s.queueManager
	s.mu.Unlock()

	queueManager, processLauncher, err := s.newQueue(scrollService, root, scrollName)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.queueManager = queueManager
	s.procedures = processLauncher
	s.mu.Unlock()

	if start {
		go queueManager.Work()
	}
	return oldQueue, nil
}
