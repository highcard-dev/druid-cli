package docker

import (
	"path/filepath"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func TestStateStorePersistsCommandStatuses(t *testing.T) {
	store, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exitCode := 2
	scroll := &domain.RuntimeScroll{
		ID:         "test",
		Artifact:   "example",
		Root:       "/tmp/root",
		ScrollName: "test",
		ScrollYAML: "name: test\n",
		Commands: map[string]domain.LockStatus{
			"start": {
				Status:           domain.ScrollLockStatusRunning,
				LastStatusChange: 10,
			},
		},
	}

	if err := store.CreateScroll(scroll); err != nil {
		t.Fatal(err)
	}

	scroll.Commands["start"] = domain.LockStatus{
		Status:           domain.ScrollLockStatusError,
		ExitCode:         &exitCode,
		LastStatusChange: 20,
	}
	scroll.Status = domain.RuntimeScrollStatusError
	if err := store.UpdateScroll(scroll); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetScroll("test")
	if err != nil {
		t.Fatal(err)
	}
	status := got.Commands["start"]
	if status.Status != domain.ScrollLockStatusError {
		t.Fatalf("status = %s, want error", status.Status)
	}
	if status.ExitCode == nil || *status.ExitCode != exitCode {
		t.Fatalf("exit code = %v, want %d", status.ExitCode, exitCode)
	}
	if status.LastStatusChange != 20 {
		t.Fatalf("last status change = %d, want 20", status.LastStatusChange)
	}
	if got.ScrollYAML != "name: test\n" {
		t.Fatalf("scroll yaml = %q, want cached yaml", got.ScrollYAML)
	}
}

func TestStateStorePersistsUIPackages(t *testing.T) {
	store, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scroll := &domain.RuntimeScroll{
		ID:         "ui",
		Artifact:   "example",
		Root:       "/tmp/root",
		ScrollName: "ui",
		ScrollYAML: "name: ui\n",
		Status:     domain.RuntimeScrollStatusCreated,
		UIPackages: domain.RuntimeUIPackages{
			domain.RuntimeUIPackageScopePrivate: {
				URL:    "http://127.0.0.1:8085/ui/private/hash/app.wasm",
				Path:   "private/dist/app.wasm",
				SHA256: "hash",
			},
		},
	}
	if err := store.CreateScroll(scroll); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetScroll("ui")
	if err != nil {
		t.Fatal(err)
	}
	pkg := got.UIPackages[domain.RuntimeUIPackageScopePrivate]
	if pkg.Path != "private/dist/app.wasm" || pkg.SHA256 != "hash" {
		t.Fatalf("ui package = %#v", pkg)
	}
}

func TestStateStoreUsesSingleRuntimeRoot(t *testing.T) {
	store, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := store.Root("scroll-a"), filepath.Join(store.StateDir(), "scrolls", "scroll-a"); got != want {
		t.Fatalf("Root = %s, want %s", got, want)
	}
}
