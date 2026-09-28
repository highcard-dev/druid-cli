package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
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
			_, err := supervisor.Update(original.ID, artifact, nil, nil)
			if err == nil || !strings.Contains(err.Error(), "accepted sha256") {
				t.Fatalf("error = %v, want accepted digest validation", err)
			}
			if backend.stopRoot != "" || backend.spawnCount != 0 {
				t.Fatal("invalid selection stopped or modified the workload")
			}
		})
	}
}

func TestUpdateRejectsChangedInstalledDescriptorBeforeStopping(t *testing.T) {
	store := newTestStateStore(t)
	fixture := &domain.RuntimeScroll{ID: "stale-update", Root: "runtime://fixture", Artifact: "registry.local/deployment:v1", ScrollYAML: cachedScrollYAML("start"), Status: domain.RuntimeScrollStatusRunning}
	if err := store.CreateScroll(fixture); err != nil {
		t.Fatal(err)
	}
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, scrollYAML: "name: registry.local/reusable\n", digest: "sha256:" + strings.Repeat("b", 64)}
	supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
	supervisor.SetWorkerCallbacks(callbacks, "http://worker-callback")
	expected := "sha256:" + strings.Repeat("a", 64)
	_, err := supervisor.Update(fixture.ID, "registry.local/deployment@sha256:"+strings.Repeat("c", 64), nil, &expected)
	if !errors.Is(err, ErrInstalledReleaseChanged) {
		t.Fatalf("stale update error: %v", err)
	}
	if backend.stopRoot != "" || backend.spawnCount != 1 || backend.action.Mode != ports.RuntimeWorkerModeInspect {
		t.Fatal("stale acceptance mutated runtime")
	}
}
