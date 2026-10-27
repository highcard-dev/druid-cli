package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

func TestMaterializationCachesInstalledReleaseAcrossRestart(t *testing.T) {
	for _, operation := range []string{"create", "update", "restore"} {
		t.Run(operation, func(t *testing.T) {
			store := newTestStateStore(t)
			callbacks := NewWorkerCallbackManager()
			digest := "sha256:" + strings.Repeat("a", 64)
			backend := &fakeWorkerBackend{callbacks: callbacks, scrollYAML: releaseScrollYAML(), digest: digest}
			supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
			supervisor.SetWorkerCallbacks(callbacks, "http://worker-callback")
			id := "cached-release"
			if operation != "create" {
				if err := store.CreateScroll(&domain.RuntimeScroll{ID: id, Root: "runtime://cached-release", Artifact: "old", ArtifactDigest: digest, InstalledReleaseState: domain.InstalledReleaseReady, ScrollYAML: cachedScrollYAML(""), Status: domain.RuntimeScrollStatusStopped}); err != nil {
					t.Fatal(err)
				}
			}
			var result *domain.RuntimeScroll
			var err error
			switch operation {
			case "create":
				result, err = supervisor.Create(t.TempDir(), id, nil)
			case "update":
				result, err = supervisor.Update(id, "registry.local/release@"+digest, nil, &digest)
			case "restore":
				result, err = supervisor.Restore(id, "registry.local/backup:v1", false, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.InstalledReleaseState != domain.InstalledReleaseReady {
				t.Fatalf("successful %s left release %q", operation, result.InstalledReleaseState)
			}
			// A new supervisor has neither callbacks nor access to the volume.
			restarted := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
			for i := 0; i < 2; i++ {
				identity, err := restarted.InstalledRelease(context.Background(), result.ID)
				if err != nil || identity["repository"] != "registry.local/release" || identity["digest"] != digest {
					t.Fatalf("cached release = %v, %v", identity, err)
				}
			}
			if backend.spawnCount != 1 {
				t.Fatalf("spawned %d workers; want only the materialization worker", backend.spawnCount)
			}
		})
	}
}

func TestInstalledReleaseRejectsUnconfirmedStateWithoutWorker(t *testing.T) {
	for _, state := range []domain.InstalledReleaseState{"", domain.InstalledReleasePending, "unknown"} {
		t.Run(string(state), func(t *testing.T) {
			store := newTestStateStore(t)
			runtime := &domain.RuntimeScroll{ID: "unconfirmed", Root: "runtime://unconfirmed", Artifact: "registry.local/backup:v1", ArtifactDigest: "sha256:" + strings.Repeat("a", 64), ScrollYAML: releaseScrollYAML(), InstalledReleaseState: state, Status: domain.RuntimeScrollStatusStopped}
			if err := store.CreateScroll(runtime); err != nil {
				t.Fatal(err)
			}
			backend := &fakeWorkerBackend{}
			supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
			for i := 0; i < 2; i++ {
				identity, err := supervisor.InstalledRelease(context.Background(), runtime.ID)
				if err == nil || !strings.Contains(err.Error(), "unconfirmed") || identity != nil {
					t.Fatalf("unconfirmed release query = %v, %v", identity, err)
				}
			}
			if backend.spawnCount != 0 {
				t.Fatal("unconfirmed release query spawned a worker")
			}
			stored, err := store.GetScroll(runtime.ID)
			if err != nil || stored.InstalledReleaseState != state || stored.ArtifactDigest != runtime.ArtifactDigest {
				t.Fatalf("query changed the recorded release: %#v, %v", stored, err)
			}
		})
	}
}

func TestInstalledReleaseRejectsInvalidConfirmedIdentityWithoutWorker(t *testing.T) {
	for _, digest := range []string{"", "sha256:invalid"} {
		t.Run(digest, func(t *testing.T) {
			store := newTestStateStore(t)
			if err := store.CreateScroll(&domain.RuntimeScroll{ID: "invalid", ScrollYAML: "name: local\n", ArtifactDigest: digest, InstalledReleaseState: domain.InstalledReleaseReady}); err != nil {
				t.Fatal(err)
			}
			backend := &fakeWorkerBackend{}
			supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
			if _, err := supervisor.InstalledRelease(context.Background(), "invalid"); err == nil || !strings.Contains(err.Error(), "invalid installed release identity") {
				t.Fatalf("invalid release query = %v", err)
			}
			if backend.spawnCount != 0 {
				t.Fatal("invalid confirmed identity spawned a worker")
			}
		})
	}
}

type failReleaseStateStore struct {
	ports.RuntimeScrollStore
	state domain.InstalledReleaseState
}

func (s *failReleaseStateStore) UpdateScroll(scroll *domain.RuntimeScroll) error {
	if scroll.InstalledReleaseState == s.state {
		return errors.New("injected release state persistence failure")
	}
	return s.RuntimeScrollStore.UpdateScroll(scroll)
}

func TestMaterializationRequiresDurableInvalidation(t *testing.T) {
	for _, operation := range []string{"update", "restore", "ensure"} {
		t.Run(operation, func(t *testing.T) {
			store := &failReleaseStateStore{RuntimeScrollStore: newTestStateStore(t), state: domain.InstalledReleasePending}
			yaml := cachedScrollYAML("")
			if operation == "ensure" {
				yaml = ""
			}
			runtime := &domain.RuntimeScroll{ID: "persist-first", Root: "runtime://persist-first", ScrollYAML: yaml, InstalledReleaseState: domain.InstalledReleaseReady, Status: domain.RuntimeScrollStatusStopped}
			if err := store.CreateScroll(runtime); err != nil {
				t.Fatal(err)
			}
			backend := &fakeWorkerBackend{}
			supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
			supervisor.SetWorkerCallbacks(NewWorkerCallbackManager(), "http://worker-callback")
			var err error
			switch operation {
			case "update":
				_, err = supervisor.Update(runtime.ID, "registry.local/release@sha256:"+strings.Repeat("a", 64), nil, nil)
			case "restore":
				_, err = supervisor.Restore(runtime.ID, "registry.local/backup:v1", false, nil)
			case "ensure":
				_, err = supervisor.Ensure(EnsureOptions{Name: runtime.ID, Artifact: t.TempDir()})
			}
			if err == nil || !strings.Contains(err.Error(), "persistence failure") || backend.spawnCount != 0 {
				t.Fatalf("materialization proceeded without durable invalidation: %v, %d workers", err, backend.spawnCount)
			}
		})
	}
}

func releaseScrollYAML() string {
	return strings.Replace(cachedScrollYAML(""), "name: cached", "name: registry.local/release", 1)
}
