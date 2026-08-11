package services

import (
	"fmt"
	"sync"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"gopkg.in/yaml.v2"
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
	devDaemonURL      string
	devAuthJWKSURL    string
	devRuntimeJWKSURL string

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
	if runtimeScroll.ScrollYAML == "" {
		return nil, fmt.Errorf("runtime scroll %s has no scroll_yaml", runtimeScroll.ID)
	}
	if runtimeService == nil {
		return nil, fmt.Errorf("runtime backend is required")
	}
	if repaired, err := repairLegacyNilCommands(runtimeScroll); err != nil {
		return nil, err
	} else if repaired {
		if err := store.UpdateScroll(runtimeScroll); err != nil {
			return nil, fmt.Errorf("repair persisted scroll commands: %w", err)
		}
	}
	scrollYAML := []byte(runtimeScroll.ScrollYAML)
	scrollService, err := coreservices.NewCachedScrollServiceWithPorts(runtimeScroll.Root, scrollYAML, reservedRuntimePorts(runtimeScroll.ReservedPorts))
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

// repairLegacyNilCommands removes only impossible null command entries written
// by older runtime state. All remaining command validation stays strict.
func repairLegacyNilCommands(runtimeScroll *domain.RuntimeScroll) (bool, error) {
	var file domain.File
	if err := yaml.Unmarshal([]byte(runtimeScroll.ScrollYAML), &file); err != nil {
		return false, fmt.Errorf("parse persisted scroll_yaml: %w", err)
	}

	repaired := false
	for name, command := range file.Commands {
		if command == nil {
			delete(file.Commands, name)
			repaired = true
		}
	}
	if !repaired {
		return false, nil
	}

	data, err := yaml.Marshal(file)
	if err != nil {
		return false, fmt.Errorf("marshal repaired scroll_yaml: %w", err)
	}
	runtimeScroll.ScrollYAML = string(data)
	return true, nil
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
