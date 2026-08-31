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

func TestWorkerCallbackTracksPullProgress(t *testing.T) {
	manager := NewWorkerCallbackManager()
	_, err := manager.Register("scroll-a")
	if err != nil {
		t.Fatal(err)
	}

	if progress, ok := manager.Progress("scroll-a"); !ok || progress != 0 {
		t.Fatalf("initial progress = %v, %v; want 0, true", progress, ok)
	}
	if err := manager.ReportProgress("scroll-a", 42); err != nil {
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

func TestWorkerCallbackRejectsProgressForUnknownRuntime(t *testing.T) {
	manager := NewWorkerCallbackManager()
	if err := manager.ReportProgress("missing", 42); err == nil {
		t.Fatal("progress for an unknown worker action should fail")
	}
}
