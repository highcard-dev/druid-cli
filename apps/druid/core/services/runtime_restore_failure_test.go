package services

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

func TestRestoreIndeterminateFailureDoesNotRestart(t *testing.T) {
	for _, failure := range []string{"spawn", "worker exit", "lost callback", "error callback"} {
		t.Run(failure, func(t *testing.T) {
			store := newTestStateStore(t)
			runtime := &domain.RuntimeScroll{
				ID: "restore-unknown", Artifact: "registry.local/original:1", Root: "runtime://restore-unknown",
				ScrollName: "original", ScrollYAML: strings.Replace(cachedScrollYAML("start"), "run: once", "run: persistent", 1),
				Status: domain.RuntimeScrollStatusRunning, Procedures: domain.ProcedureStatusMap{},
			}
			if err := store.CreateScroll(runtime); err != nil {
				t.Fatal(err)
			}
			callbacks := NewWorkerCallbackManager()
			backend := &fakeWorkerBackend{procedureStatusUpdates: []ports.ProcedureStatusUpdate{{Procedure: "start.0", Status: domain.ScrollLockStatusRunning}}}
			switch failure {
			case "spawn":
				backend.workerErr = errors.New("worker creation response lost")
			case "worker exit":
				done := make(chan error, 1)
				done <- errors.New("worker killed during root replacement")
				backend.workerDone = done
			case "error callback":
				backend.callbacks = callbacks
				backend.workerResult = &ports.RuntimeWorkerResult{Error: "worker failed without recovery classification"}
			}
			supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
			supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")
			supervisor.SetWorkerTimeout(20 * time.Millisecond)
			if _, err := supervisor.Restore(runtime.ID, "registry.local/backups:1", true, nil); err == nil {
				t.Fatal("expected worker failure")
			}
			stored, err := store.GetScroll(runtime.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != domain.RuntimeScrollStatusError || stored.LastError == "" {
				t.Fatalf("unknown restore outcome must remain stopped with an error, got status=%s error=%q", stored.Status, stored.LastError)
			}
		})
	}
}
