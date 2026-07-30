package services

import (
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/ports"
)

func TestWorkerCallbackValidatesTokenAndRejectsReplay(t *testing.T) {
	manager := NewWorkerCallbackManager()
	token, resultCh, err := manager.Register("scroll-a")
	if err != nil {
		t.Fatal(err)
	}
	result := ports.RuntimeWorkerResult{ScrollYAML: "name: scroll-a\n"}
	if err := manager.Complete("scroll-a", "wrong-token", result); err == nil {
		t.Fatal("invalid token should fail")
	}
	if err := manager.Complete("scroll-a", token, result); err != nil {
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
	if err := manager.Complete("scroll-a", token, result); err == nil {
		t.Fatal("replayed callback should fail")
	}
}

func TestWorkerCallbackRejectsDuplicatePendingRuntime(t *testing.T) {
	manager := NewWorkerCallbackManager()
	if _, _, err := manager.Register("scroll-a"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Register("scroll-a"); err == nil {
		t.Fatal("duplicate pending action should fail")
	}
}

func TestWorkerCallbackRejectsUnknownRuntime(t *testing.T) {
	manager := NewWorkerCallbackManager()
	token, _, err := manager.Register("scroll-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Complete("scroll-b", token, ports.RuntimeWorkerResult{}); err == nil {
		t.Fatal("unknown runtime should fail")
	}
}

func TestWorkerCallbackTracksPullProgress(t *testing.T) {
	manager := NewWorkerCallbackManager()
	token, _, err := manager.Register("scroll-a")
	if err != nil {
		t.Fatal(err)
	}

	if progress, ok := manager.Progress("scroll-a"); !ok || progress != 0 {
		t.Fatalf("initial progress = %v, %v; want 0, true", progress, ok)
	}
	if err := manager.ReportProgress("scroll-a", "wrong-token", 42); err == nil {
		t.Fatal("invalid progress token should fail")
	}
	if err := manager.ReportProgress("scroll-a", token, 42); err != nil {
		t.Fatal(err)
	}
	if progress, ok := manager.Progress("scroll-a"); !ok || progress != 42 {
		t.Fatalf("reported progress = %v, %v; want 42, true", progress, ok)
	}

	manager.Cancel("scroll-a")
	if _, ok := manager.Progress("scroll-a"); ok {
		t.Fatal("cancelled progress should be removed")
	}
}
