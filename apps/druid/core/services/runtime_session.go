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
	watchService      ports.WatchServiceInterface
	runtimeBackend    ports.RuntimeBackendInterface
	queue             map[string]*runtimeQueueItem
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
	if runtimeService == nil {
		return nil, fmt.Errorf("runtime backend is required")
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
	s.triggerRunQueue()
}
