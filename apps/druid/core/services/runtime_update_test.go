package services

import (
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

func TestRuntimeUpdateRejectsUnacceptedReferenceBeforeStopping(t *testing.T) {
	for _, artifact := range []string{"", "registry.local/example:latest", "registry.local/example@sha256:bad"} {
		t.Run(artifact, func(t *testing.T) {
			store := newTestStateStore(t)
			original := &domain.RuntimeScroll{ID: "accepted-update", Root: "runtime://accepted-update", Artifact: "registry.local/example:v1", ScrollYAML: cachedScrollYAML("start"), Status: domain.RuntimeScrollStatusRunning}
			if err := store.CreateScroll(original); err != nil {
				t.Fatal(err)
			}
			backend := &fakeWorkerBackend{}
			supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
			_, err := supervisor.Update(original.ID, artifact, nil)
			if err == nil || !strings.Contains(err.Error(), "accepted sha256") {
				t.Fatalf("error = %v, want accepted digest validation", err)
			}
			if backend.stopRoot != "" || backend.spawnCount != 0 {
				t.Fatal("invalid selection stopped or modified the workload")
			}
		})
	}
}
