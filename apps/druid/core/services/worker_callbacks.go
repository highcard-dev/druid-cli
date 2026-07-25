package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type WorkerCallbackManager struct {
	mu       sync.Mutex
	actions  map[string]workerCallbackAction
	progress map[string]workerCallbackProgress
}

type workerCallbackAction struct {
	token  string
	result chan ports.RuntimeWorkerResult
}

type workerCallbackProgress struct {
	snapshot *domain.SnapshotProgress
	trackers int
}

func NewWorkerCallbackManager() *WorkerCallbackManager {
	return &WorkerCallbackManager{
		actions:  map[string]workerCallbackAction{},
		progress: map[string]workerCallbackProgress{},
	}
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
	progress := m.progress[runtimeID]
	if progress.snapshot == nil {
		progress.snapshot = domain.NewSnapshotProgress()
	}
	progress.snapshot.StorePercentage(0)
	m.progress[runtimeID] = progress
	m.mu.Unlock()
	return token, ch, nil
}

func (m *WorkerCallbackManager) Cancel(runtimeID string) {
	m.mu.Lock()
	delete(m.actions, runtimeID)
	if progress := m.progress[runtimeID]; progress.trackers == 0 {
		delete(m.progress, runtimeID)
	}
	m.mu.Unlock()
}

func (m *WorkerCallbackManager) ReportProgress(runtimeID string, token string, percentage float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	action, ok := m.actions[runtimeID]
	if !ok {
		return fmt.Errorf("unknown or completed worker action")
	}
	if token == "" || token != action.token {
		return fmt.Errorf("invalid worker token")
	}
	if !m.progress[runtimeID].snapshot.StorePercentage(percentage) {
		return fmt.Errorf("invalid percentage")
	}
	return nil
}

func (m *WorkerCallbackManager) Progress(runtimeID string) (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	progress, ok := m.progress[runtimeID]
	if !ok {
		return 0, false
	}
	return progress.snapshot.Percentage(), true
}

func (m *WorkerCallbackManager) TrackSnapshotProgress(runtimeID string) *domain.SnapshotProgress {
	m.mu.Lock()
	progress := m.progress[runtimeID]
	if progress.snapshot == nil {
		progress.snapshot = domain.NewSnapshotProgress()
	}
	progress.trackers++
	m.progress[runtimeID] = progress
	m.mu.Unlock()
	return progress.snapshot
}

func (m *WorkerCallbackManager) ClearSnapshotProgress(runtimeID string, progress *domain.SnapshotProgress) {
	m.mu.Lock()
	current, ok := m.progress[runtimeID]
	if ok && current.snapshot == progress {
		if current.trackers > 0 {
			current.trackers--
		}
		if _, pending := m.actions[runtimeID]; pending || current.trackers > 0 {
			m.progress[runtimeID] = current
		} else {
			delete(m.progress, runtimeID)
		}
	}
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
	if progress := m.progress[runtimeID]; progress.trackers == 0 {
		delete(m.progress, runtimeID)
	}
	m.mu.Unlock()
	action.result <- result
	close(action.result)
	return nil
}
