package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/highcard-dev/daemon/internal/core/ports"
)

type WorkerCallbackManager struct {
	mu      sync.Mutex
	actions map[string]workerCallbackAction
}

type workerCallbackAction struct {
	token  string
	result chan ports.RuntimeWorkerResult
}

func NewWorkerCallbackManager() *WorkerCallbackManager {
	return &WorkerCallbackManager{actions: map[string]workerCallbackAction{}}
}

func (m *WorkerCallbackManager) Register(runtimeID string) (string, <-chan ports.RuntimeWorkerResult, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", nil, err
	}
	token := hex.EncodeToString(tokenBytes)
	ch := make(chan ports.RuntimeWorkerResult, 1)
	m.mu.Lock()
	if _, ok := m.actions[runtimeID]; ok {
		m.mu.Unlock()
		return "", nil, fmt.Errorf("worker action already pending for runtime %s", runtimeID)
	}
	m.actions[runtimeID] = workerCallbackAction{token: token, result: ch}
	m.mu.Unlock()
	return token, ch, nil
}

func (m *WorkerCallbackManager) Cancel(runtimeID string) {
	m.mu.Lock()
	delete(m.actions, runtimeID)
	m.mu.Unlock()
}

func (m *WorkerCallbackManager) Complete(runtimeID string, token string, result ports.RuntimeWorkerResult) error {
	m.mu.Lock()
	action, ok := m.actions[runtimeID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown or completed worker action")
	}
	if token == "" || token != action.token {
		m.mu.Unlock()
		return fmt.Errorf("invalid worker token")
	}
	delete(m.actions, runtimeID)
	m.mu.Unlock()
	action.result <- result
	close(action.result)
	return nil
}
