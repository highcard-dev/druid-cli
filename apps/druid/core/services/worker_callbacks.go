package services

import (
	"fmt"
	"sync"

	"github.com/highcard-dev/daemon/internal/core/ports"
)

type WorkerCallbackManager struct {
	mu      sync.Mutex
	actions map[string]workerCallbackAction
}

type workerCallbackAction struct {
	result chan ports.RuntimeWorkerResult
}

func NewWorkerCallbackManager() *WorkerCallbackManager {
	return &WorkerCallbackManager{
		actions: map[string]workerCallbackAction{},
	}
}

func (m *WorkerCallbackManager) Register(runtimeID string) (<-chan ports.RuntimeWorkerResult, error) {
	ch := make(chan ports.RuntimeWorkerResult, 1)
	m.mu.Lock()
	if _, ok := m.actions[runtimeID]; ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("worker action already pending for runtime %s", runtimeID)
	}
	m.actions[runtimeID] = workerCallbackAction{result: ch}
	m.mu.Unlock()
	return ch, nil
}

func (m *WorkerCallbackManager) Cancel(runtimeID string) {
	m.mu.Lock()
	delete(m.actions, runtimeID)
	m.mu.Unlock()
}

func (m *WorkerCallbackManager) Complete(runtimeID string, result ports.RuntimeWorkerResult) error {
	m.mu.Lock()
	action, ok := m.actions[runtimeID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown or completed worker action")
	}
	delete(m.actions, runtimeID)
	m.mu.Unlock()
	action.result <- result
	close(action.result)
	return nil
}
