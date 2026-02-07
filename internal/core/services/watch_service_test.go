package services

import (
	"context"
	"os"
	"testing"
	"time"

	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

func TestWatchService_BasicFunctionality(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queueManager := mock_ports.NewMockQueueManagerInterface(ctrl)
	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)

	// Create the UI dev service
	uiDevService := NewUiDevService(queueManager, scrollService)

	// Check initial state
	if uiDevService.IsWatching() {
		t.Error("Service should not be watching initially")
	}

	if len(uiDevService.GetWatchedPaths()) != 0 {
		t.Error("Should have no watched paths initially")
	}

	// Start watching
	err := uiDevService.StartWatching("/tmp/test", "/tmp/test/ui")
	if err != nil {
		t.Logf("Expected error when watching non-existent path: %v", err)
	}

	// Subscribe to changes
	changesChan := uiDevService.Subscribe()
	if changesChan == nil {
		t.Error("Subscribe should return a valid channel")
	}

	// Test subscription context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	select {
	case <-changesChan:
		// This is not expected in this test since we're not making actual file changes
		t.Log("Received change notification")
	case <-ctx.Done():
		// This is expected - no changes should be received
		t.Log("No changes received as expected")
	}

	// Unsubscribe
	uiDevService.Unsubscribe(changesChan)

	// Stop watching
	err = uiDevService.StopWatching()
	if err != nil {
		t.Errorf("Failed to stop watching: %v", err)
	}

	if uiDevService.IsWatching() {
		t.Error("Service should not be watching after stop")
	}
}

func TestWatchService_MultipleSubscribers(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queueManager := mock_ports.NewMockQueueManagerInterface(ctrl)
	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)

	// Create the UI dev service
	uiDevService := NewUiDevService(queueManager, scrollService)

	// Start watching first
	err := uiDevService.StartWatching("/tmp/test", "/tmp/test/ui")
	if err != nil {
		t.Logf("Expected error when watching non-existent path: %v", err)
	}

	// Create multiple subscribers
	sub1 := uiDevService.Subscribe()
	sub2 := uiDevService.Subscribe()
	sub3 := uiDevService.Subscribe()

	if sub1 == nil || sub2 == nil || sub3 == nil {
		t.Error("All subscribers should be valid channels")
	}

	// Unsubscribe all
	uiDevService.Unsubscribe(sub1)
	uiDevService.Unsubscribe(sub2)
	uiDevService.Unsubscribe(sub3)

	// Stop the service
	uiDevService.StopWatching()
}

func TestWatchService_ContinuousStartStop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queueManager := mock_ports.NewMockQueueManagerInterface(ctrl)
	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)

	// Create the UI dev service
	uiDevService := NewUiDevService(queueManager, scrollService)

	// Test multiple start/stop cycles
	for i := 0; i < 5; i++ {
		t.Logf("Start/Stop cycle %d", i+1)

		// Verify initial state
		if uiDevService.IsWatching() {
			t.Errorf("Cycle %d: Service should not be watching initially", i+1)
		}

		// Start watching
		err := uiDevService.StartWatching("/tmp/test", "/tmp/test/ui")
		if err != nil {
			t.Logf("Cycle %d: Expected error when watching non-existent path: %v", i+1, err)
		}

		// Verify running state
		if !uiDevService.IsWatching() {
			t.Errorf("Cycle %d: Service should be watching after start", i+1)
		}

		// Test subscription
		sub := uiDevService.Subscribe()
		if sub == nil {
			t.Errorf("Cycle %d: Subscribe should return a valid channel", i+1)
		}

		// Quick context test
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		select {
		case <-sub:
			t.Logf("Cycle %d: Received unexpected change notification", i+1)
		case <-ctx.Done():
			// Expected - no changes
		}
		cancel()

		// Unsubscribe
		uiDevService.Unsubscribe(sub)

		// Stop watching
		err = uiDevService.StopWatching()
		if err != nil {
			t.Errorf("Cycle %d: Failed to stop watching: %v", i+1, err)
		}

		// Verify stopped state
		if uiDevService.IsWatching() {
			t.Errorf("Cycle %d: Service should not be watching after stop", i+1)
		}

		if len(uiDevService.GetWatchedPaths()) != 0 {
			t.Errorf("Cycle %d: Should have no watched paths after stop", i+1)
		}

		// Test that Subscribe returns nil when not watching
		nullSub := uiDevService.Subscribe()
		if nullSub != nil {
			t.Errorf("Cycle %d: Subscribe should return nil when not watching", i+1)
		}
	}
}

func TestWatchService_SubscribeBeforeStart(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queueManager := mock_ports.NewMockQueueManagerInterface(ctrl)
	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)

	// Create the UI dev service
	uiDevService := NewUiDevService(queueManager, scrollService)

	// Try to subscribe before starting
	sub := uiDevService.Subscribe()
	if sub != nil {
		t.Error("Subscribe should return nil when service is not started")
	}

	// Try to unsubscribe with nil channel (should not panic)
	uiDevService.Unsubscribe(nil)
	uiDevService.Unsubscribe(sub) // sub is nil, should handle gracefully
}

func TestWatchService_RelativePathsJoinedWithBasePath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	queueManager := mock_ports.NewMockQueueManagerInterface(ctrl)
	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)

	// Create a temp directory structure for testing
	tempDir := t.TempDir()
	srcDir := tempDir + "/src"
	configDir := tempDir + "/config"

	// Create the subdirectories
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create src directory: %v", err)
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}

	// Create the UI dev service
	uiDevService := NewUiDevService(queueManager, scrollService)

	// Start watching with relative paths (simulating what the handler does)
	err := uiDevService.StartWatching(tempDir, "src", "config")
	if err != nil {
		t.Fatalf("Failed to start watching: %v", err)
	}
	defer uiDevService.StopWatching()

	// Verify the watched paths are absolute and properly joined
	watchedPaths := uiDevService.GetWatchedPaths()

	if len(watchedPaths) != 2 {
		t.Fatalf("Expected 2 watched paths, got %d: %v", len(watchedPaths), watchedPaths)
	}

	// GetWatchedPaths returns paths relative to basePath, so they should be "src" and "config"
	expectedPaths := map[string]bool{"src": false, "config": false}
	for _, path := range watchedPaths {
		if _, ok := expectedPaths[path]; ok {
			expectedPaths[path] = true
		} else {
			t.Errorf("Unexpected watched path: %s", path)
		}
	}

	for path, found := range expectedPaths {
		if !found {
			t.Errorf("Expected path '%s' not found in watched paths", path)
		}
	}

	// Verify that files created in the watched directories trigger events
	sub := uiDevService.Subscribe()
	if sub == nil {
		t.Fatal("Subscribe should return a valid channel")
	}
	defer uiDevService.Unsubscribe(sub)

	// Create a test file in the src directory
	testFile := srcDir + "/test.txt"
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Wait for the file change event
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	select {
	case event := <-sub:
		if event == nil {
			t.Error("Received nil event")
		}
		// Successfully received a file change event, which proves the watcher
		// is correctly watching the joined path (basePath + relative path)
		t.Logf("Received file change event: %s", string(*event))
	case <-ctx.Done():
		t.Error("Timeout waiting for file change event - relative path was likely not joined with base path")
	}
}
