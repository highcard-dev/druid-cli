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
	queue             map[string]*runtimeQueueItem
	taskChan          chan string
	taskDoneChan      chan struct{}
	shutdownChan      chan struct{}
	shutdownDoneChan  chan struct{}
	shutdownOnce      sync.Once
	workWg            sync.WaitGroup
	notifierChan      []chan []string
	devWatchPaths     []string
	devCommands       []string
	devDaemonURL      string
	devDaemonToken    string
	devAuthJWKSURL    string
	devRuntimeJWKSURL string
	devWatchBridge    *domain.BroadcastChannel
	devWatchCancel    context.CancelFunc

	mu      sync.Mutex
	queueMu sync.Mutex
	runMu   sync.Mutex
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
	session.resetQueueState()
	queueManager, processLauncher, err := session.newQueue(scrollService, runtimeScroll.Root, runtimeScroll.ScrollName)
	if err != nil {
		return nil, err
	}
	session.queueManager = queueManager
	session.procedures = processLauncher
	return session, nil
}

func (s *RuntimeSession) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	s.startQueue()
}

func (s *RuntimeSession) Shutdown() {
	s.shutdownQueueLoop()
	s.mu.Lock()
	s.started = false
	s.mu.Unlock()
}

func (s *RuntimeSession) startQueue() {
	s.resetQueueState()
	go s.Work()
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
	processLauncher.SetProcedureStatusObserver(s.persistProcedureStatus)
	queueManager := coreservices.NewQueueManager(scrollService, processLauncher)
	return queueManager, processLauncher, nil
}

func (s *RuntimeSession) replaceQueue(start bool) error {
	s.mu.Lock()
	scrollService := s.scrollService
	root := s.runtimeScroll.Root
	scrollName := s.runtimeScroll.ScrollName
	s.mu.Unlock()

	queueManager, processLauncher, err := s.newQueue(scrollService, root, scrollName)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.queueManager = queueManager
	s.procedures = processLauncher
	s.mu.Unlock()

	if start {
		s.startQueue()
	} else {
		s.resetQueueState()
	}
	return nil
}
