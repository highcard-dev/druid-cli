package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

// FileChangeEvent represents a file system change event
type FileChangeEvent struct {
	Path      string    `json:"path"`
	Operation string    `json:"operation"`
	Timestamp time.Time `json:"timestamp"`
}

type CommandDoneEvent struct {
	CommandKey string    `json:"command_key"`
	Timestamp  time.Time `json:"timestamp"`
}

// UiDevService handles file watching and change notifications for UI development
type UiDevService struct {
	watcher          *fsnotify.Watcher
	broadcastChannel *domain.BroadcastChannel
	watchPaths       []string
	basePath         string // Base path for making relative paths
	mu               sync.RWMutex
	ctx              context.Context
	cancel           context.CancelFunc
	isWatching       bool
	commands         map[string]*domain.CommandInstructionSet
	queueManager     ports.QueueManagerInterface
	scrollService    ports.ScrollServiceInterface
	buildActive      bool
	changeAfterBuild bool
}

// NewUiDevService creates a new instance of UiDevService
func NewUiDevService(
	queueManager ports.QueueManagerInterface, scrollService ports.ScrollServiceInterface,
) ports.UiDevServiceInterface {
	return &UiDevService{
		watchPaths:    make([]string, 0),
		isWatching:    false,
		queueManager:  queueManager,
		scrollService: scrollService,
	}
}

func (uds *UiDevService) SetCommands(commands map[string]*domain.CommandInstructionSet) {
	uds.mu.Lock()
	defer uds.mu.Unlock()
	uds.commands = commands
	for key, cmd := range commands {
		uds.scrollService.AddTemporaryCommand(key, cmd)
	}
}

// StartWatching initializes the file watcher and starts monitoring specified paths
func (uds *UiDevService) StartWatching(basePath string, paths ...string) error {
	uds.mu.Lock()
	defer uds.mu.Unlock()

	if uds.isWatching {
		logger.Log().Info("File watcher is already running")
		return nil
	}

	// Create new context and cancel function for this session
	uds.ctx, uds.cancel = context.WithCancel(context.Background())

	// Create new broadcast channel for this session
	uds.broadcastChannel = domain.NewHub()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Log().Error("Failed to create file watcher", zap.Error(err))
		return err
	}

	uds.watcher = watcher
	uds.isWatching = true

	// Set base path for relative path calculation
	uds.basePath = basePath

	// Add default paths if none provided
	if len(paths) == 0 {
		return errors.New("no paths provided to watch")
	}

	// Add paths to watcher
	for _, path := range paths {
		if err := uds.addWatchPath(path); err != nil {
			logger.Log().Warn("Failed to watch path", zap.String("path", path), zap.Error(err))
		} else {
			uds.watchPaths = append(uds.watchPaths, path)
			logger.Log().Info("Watching path", zap.String("path", path))
		}
	}

	// Start the event processing goroutine
	go uds.processEvents()

	// Start the broadcast hub
	go uds.broadcastChannel.Run()

	logger.Log().Info("UI dev file watcher started")
	return nil
}

// StopWatching stops the file watcher and cleans up resources
func (uds *UiDevService) StopWatching() error {
	uds.mu.Lock()
	defer uds.mu.Unlock()

	if !uds.isWatching {
		return nil
	}

	// Cancel context to stop goroutines
	if uds.cancel != nil {
		uds.cancel()
	}

	// Close the file watcher
	if uds.watcher != nil {
		err := uds.watcher.Close()
		uds.watcher = nil
		if err != nil {
			logger.Log().Error("Failed to close file watcher", zap.Error(err))
			return err
		}
	}

	// Close the broadcast channel to clean up subscribers
	if uds.broadcastChannel != nil {
		uds.broadcastChannel.Close()
		uds.broadcastChannel = nil
	}

	uds.isWatching = false
	uds.watchPaths = make([]string, 0)
	uds.ctx = nil
	uds.cancel = nil

	logger.Log().Info("UI dev file watcher stopped")
	return nil
}

// Subscribe returns a channel for receiving file change notifications
func (uds *UiDevService) Subscribe() chan *[]byte {
	uds.mu.RLock()
	defer uds.mu.RUnlock()

	if uds.broadcastChannel == nil {
		logger.Log().Warn("Cannot subscribe: broadcast channel not initialized. Start watching first.")
		return nil
	}

	return uds.broadcastChannel.Subscribe()
}

// Unsubscribe removes a client from receiving file change notifications
func (uds *UiDevService) Unsubscribe(client chan *[]byte) {
	uds.mu.RLock()
	defer uds.mu.RUnlock()

	if uds.broadcastChannel != nil && client != nil {
		uds.broadcastChannel.Unsubscribe(client)
	}
}

// GetWatchedPaths returns the list of currently watched paths (relative to base path)
func (uds *UiDevService) GetWatchedPaths() []string {
	uds.mu.RLock()
	defer uds.mu.RUnlock()

	paths := make([]string, len(uds.watchPaths))
	for i, path := range uds.watchPaths {
		if relPath, err := filepath.Rel(uds.basePath, path); err == nil {
			paths[i] = relPath
		} else {
			// Fallback to original path if relative path calculation fails
			paths[i] = path
		}
	}
	return paths
}

// IsWatching returns whether the file watcher is currently active
func (uds *UiDevService) IsWatching() bool {
	uds.mu.RLock()
	defer uds.mu.RUnlock()
	return uds.isWatching
}

// addWatchPath adds a path to the file watcher, including subdirectories
func (uds *UiDevService) addWatchPath(path string) error {
	return filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip paths that can't be accessed
		}

		if info.IsDir() {
			// Skip node_modules directories
			if info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return uds.watcher.Add(walkPath)
		}
		return nil
	})
}

// processEvents handles file system events and broadcasts them to subscribers
func (uds *UiDevService) processEvents() {
	defer func() {
		if r := recover(); r != nil {
			logger.Log().Error("File watcher panic recovered", zap.Any("error", r))
		}
	}()

	for {
		select {
		case <-uds.ctx.Done():
			logger.Log().Info("File watcher context cancelled")
			return

		case event, ok := <-uds.watcher.Events:
			if !ok {
				logger.Log().Info("File watcher events channel closed")
				return
			}
			if event.Op&fsnotify.Chmod == fsnotify.Chmod {
				// Ignore chmod events
				continue
			}
			uds.handleFileEvent(event)

		case err, ok := <-uds.watcher.Errors:
			if !ok {
				logger.Log().Info("File watcher errors channel closed")
				return
			}

			logger.Log().Error("File watcher error", zap.Error(err))
		}
	}
}

// handleFileEvent processes a single file system event and broadcasts it
func (uds *UiDevService) handleFileEvent(event fsnotify.Event) {
	// Convert absolute path to relative path
	uds.mu.RLock()
	basePath := uds.basePath
	broadcastChannel := uds.broadcastChannel
	uds.mu.RUnlock()

	relativePath := event.Name
	if basePath != "" {
		if relPath, err := filepath.Rel(basePath, event.Name); err == nil {
			relativePath = relPath
		}
	}

	changeEvent := FileChangeEvent{
		Path:      relativePath,
		Operation: event.Op.String(),
		Timestamp: time.Now(),
	}

	// Convert to JSON for broadcasting
	eventData, err := json.Marshal(changeEvent)
	if err != nil {
		logger.Log().Error("Failed to marshal file change event", zap.Error(err))
		return
	}

	// Broadcast the event to all subscribers
	if broadcastChannel != nil {
		if !broadcastChannel.Broadcast(eventData) {
			// Silently drop if channel is full - this is normal during high activity
			logger.Log().Debug("Dropped file change event (channel busy)", zap.String("path", event.Name))
		}
	}

	// Handle directory creation - add new directories to watcher
	if event.Op&fsnotify.Create == fsnotify.Create {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			uds.mu.RLock()
			watcher := uds.watcher
			uds.mu.RUnlock()

			if watcher != nil {
				if err := watcher.Add(event.Name); err != nil {
					logger.Log().Warn("Failed to add new directory to watcher", zap.String("path", event.Name), zap.Error(err))
				} else {
					logger.Log().Info("Added new directory to watcher", zap.String("path", event.Name))
				}
			}
		}
	}

	// Handle build commands in a separate goroutine to avoid blocking the event loop
	go uds.handleBuildCommands()
}

// handleBuildCommands processes build commands with proper synchronization
func (uds *UiDevService) handleBuildCommands() {
	uds.mu.Lock()

	// Prevent overlapping builds - if build is active, mark that a change occurred
	if uds.buildActive {
		uds.changeAfterBuild = true
		uds.mu.Unlock()
		return
	}

	// Check if there are commands to execute
	if uds.commands == nil || len(uds.commands) == 0 {
		uds.mu.Unlock()
		return
	}

	// Mark build as active and get snapshot of commands
	uds.buildActive = true
	commands := make(map[string]*domain.CommandInstructionSet, len(uds.commands))
	for key, cmd := range uds.commands {
		commands[key] = cmd
	}
	broadcastChannel := uds.broadcastChannel
	uds.mu.Unlock()

	broadcastEvent := func() {
		if broadcastChannel == nil {
			return
		}
		var cmdDoneEvent = CommandDoneEvent{
			CommandKey: "file-change-event",
			Timestamp:  time.Now(),
		}
		eventCmdData, err := json.Marshal(cmdDoneEvent)
		if err != nil {
			logger.Log().Error("Failed to marshal command done event", zap.Error(err))
			return
		}
		broadcastChannel.Broadcast(eventCmdData)
	}

	for key := range commands {
		uds.queueManager.AddTempItemWithWait(key)
		broadcastEvent()

		// Check if changes occurred during build
		uds.mu.Lock()
		for uds.changeAfterBuild {
			uds.changeAfterBuild = false
			uds.mu.Unlock()

			uds.queueManager.AddTempItemWithWait(key)
			broadcastEvent()

			uds.mu.Lock()
		}
		uds.mu.Unlock()
	}

	uds.mu.Lock()
	uds.buildActive = false
	uds.mu.Unlock()
}
