package services

import (
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
	store             coreservices.RuntimeScrollStore
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

	mu      sync.Mutex
	started bool
}

func NewRuntimeSession(
	store coreservices.RuntimeScrollStore,
	runtimeScroll *domain.RuntimeScroll,
	runtimeService ports.RuntimeBackendInterface,
) (*RuntimeSession, error) {
	if runtimeScroll.Root == "" {
		return nil, fmt.Errorf("runtime scroll %s has no root", runtimeScroll.ID)
	}
	scrollYAML := []byte(runtimeScroll.ScrollYAML)
	if len(scrollYAML) == 0 {
		var err error
		scrollYAML, err = runtimeService.ReadScrollFile(runtimeScroll.Root)
		if err != nil {
			return nil, err
		}
		runtimeScroll.ScrollYAML = string(scrollYAML)
		if err := store.UpdateScroll(runtimeScroll); err != nil {
			return nil, err
		}
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
	processLauncher, err := coreservices.NewProcedureLauncherForRuntime(scrollService, runtimeService, runtimeScroll.Root, runtimeScroll.ID, runtimeScroll.ScrollName, func() []domain.RuntimeRouteAssignment {
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
