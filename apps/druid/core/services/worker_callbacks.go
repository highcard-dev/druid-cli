package services

import (
	"fmt"
	"sync"

	"github.com/highcard-dev/daemon/internal/core/ports"
)

type WorkerCallbackManager struct {
	mu       sync.Mutex
	actions  map[string]workerCallbackAction
	progress map[string]int64
}

type workerCallbackAction struct {
	result chan ports.RuntimeWorkerResult
}

func NewWorkerCallbackManager() *WorkerCallbackManager {
	return &WorkerCallbackManager{
		actions:  map[string]workerCallbackAction{},
		progress: map[string]int64{},
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
	m.progress[runtimeID] = 0
	m.mu.Unlock()
	return ch, nil
}

func (m *WorkerCallbackManager) Cancel(runtimeID string) {
	m.mu.Lock()
	delete(m.actions, runtimeID)
	delete(m.progress, runtimeID)
	m.mu.Unlock()
}

func (m *WorkerCallbackManager) ReportProgress(runtimeID string, percentage int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.actions[runtimeID]; !ok {
		return fmt.Errorf("unknown or completed worker action")
	}
	m.progress[runtimeID] = max(0, min(100, percentage))
	return nil
}

func (m *WorkerCallbackManager) Progress(runtimeID string) (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	progress, ok := m.progress[runtimeID]
	return float64(progress), ok
}

func (m *WorkerCallbackManager) Complete(runtimeID string, result ports.RuntimeWorkerResult) error {
	m.mu.Lock()
	action, ok := m.actions[runtimeID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown or completed worker action")
	}
	delete(m.actions, runtimeID)
	delete(m.progress, runtimeID)
	m.mu.Unlock()
	action.result <- result
	close(action.result)
	return nil
}
