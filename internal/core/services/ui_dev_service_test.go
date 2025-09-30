package services

import (
	"context"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
)

func TestUiDevService_BasicFunctionality(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create the UI dev service
	uiDevService := NewUiDevService()

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

func TestUiDevService_MultipleSubscribers(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	uiDevService := NewUiDevService()

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

func TestUiDevService_ContinuousStartStop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	uiDevService := NewUiDevService()

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

func TestUiDevService_SubscribeBeforeStart(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	uiDevService := NewUiDevService()

	// Try to subscribe before starting
	sub := uiDevService.Subscribe()
	if sub != nil {
		t.Error("Subscribe should return nil when service is not started")
	}

	// Try to unsubscribe with nil channel (should not panic)
	uiDevService.Unsubscribe(nil)
	uiDevService.Unsubscribe(sub) // sub is nil, should handle gracefully
}
