package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

const fileWatchInterval = 250 * time.Millisecond

type FileChangeEvent struct {
	Path      string    `json:"path"`
	Operation string    `json:"operation"`
	Timestamp time.Time `json:"timestamp"`
}

// WatchService detects changes to specific files, including files created or
// atomically replaced after the watcher starts.
type WatchService struct {
	root  string
	mu    sync.Mutex
	hub   *domain.BroadcastChannel
	stop  chan struct{}
	paths []string
}

func NewWatchService(root string) *WatchService {
	return &WatchService{root: root}
}

func (s *WatchService) StartWatching(paths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hub != nil && slices.Equal(paths, s.paths) {
		return
	}
	s.stopWatching()

	paths = slices.Clone(paths)
	files := make([]os.FileInfo, len(paths))
	for i, path := range paths {
		files[i] = s.statFile(path)
	}
	s.hub = domain.NewHub()
	s.stop = make(chan struct{})
	s.paths = paths
	go s.hub.Run()
	go s.watchFiles(s.stop, s.hub, paths, files)
}

func (s *WatchService) StopWatching() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopWatching()
}

func (s *WatchService) stopWatching() {
	if s.hub != nil {
		close(s.stop)
		s.hub.Close()
		s.hub = nil
		s.stop = nil
		s.paths = nil
	}
}

func (s *WatchService) IsWatching() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hub != nil
}

func (s *WatchService) Subscribe() chan *[]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hub == nil {
		return nil
	}
	return s.hub.Subscribe()
}

func (s *WatchService) Unsubscribe(client chan *[]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hub != nil {
		s.hub.Unsubscribe(client)
	}
}

func (s *WatchService) statFile(path string) os.FileInfo {
	info, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(path)))
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	return info
}

func (s *WatchService) watchFiles(stop <-chan struct{}, hub *domain.BroadcastChannel, paths []string, files []os.FileInfo) {
	ticker := time.NewTicker(fileWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			for i, path := range paths {
				current := s.statFile(path)
				operation := fileChangeOperation(files[i], current)
				files[i] = current
				if operation == "" {
					continue
				}
				data, err := json.Marshal(FileChangeEvent{Path: path, Operation: operation, Timestamp: now})
				if err == nil {
					hub.Broadcast(data)
				}
			}
		}
	}
}

func fileChangeOperation(previous, current os.FileInfo) string {
	switch {
	case previous == nil && current != nil:
		return "CREATE"
	case previous != nil && current == nil:
		return "REMOVE"
	case previous != nil && current != nil &&
		(!os.SameFile(previous, current) || previous.Size() != current.Size() || !previous.ModTime().Equal(current.ModTime())):
		return "WRITE"
	default:
		return ""
	}
}
