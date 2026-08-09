package services

import (
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/ports"
)

func TestWorkerCallbackCompletesOnceAndRejectsReplay(t *testing.T) {
	manager := NewWorkerCallbackManager()
	resultCh, err := manager.Register("scroll-a")
	if err != nil {
		t.Fatal(err)
	}
	result := ports.RuntimeWorkerResult{ScrollYAML: "name: scroll-a\n"}
	if err := manager.Complete("scroll-a", result); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-resultCh:
		if got.ScrollYAML != result.ScrollYAML {
			t.Fatalf("result = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("callback result was not delivered")
	}
	if err := manager.Complete("scroll-a", result); err == nil {
		t.Fatal("replayed callback should fail")
	}
}

func TestWorkerCallbackRejectsDuplicatePendingRuntime(t *testing.T) {
	manager := NewWorkerCallbackManager()
	if _, err := manager.Register("scroll-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Register("scroll-a"); err == nil {
		t.Fatal("duplicate pending action should fail")
	}
}

func TestWorkerCallbackRejectsUnknownRuntime(t *testing.T) {
	manager := NewWorkerCallbackManager()
	_, err := manager.Register("scroll-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Complete("scroll-b", ports.RuntimeWorkerResult{}); err == nil {
		t.Fatal("unknown runtime should fail")
	}
}
