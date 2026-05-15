package docker

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	_ "modernc.org/sqlite"
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

func TestStateStoreUsesSingleRuntimeRoot(t *testing.T) {
	store, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := store.Root("scroll-a"), filepath.Join(store.StateDir(), "scrolls", "scroll-a"); got != want {
		t.Fatalf("Root = %s, want %s", got, want)
	}
}

func TestStateStoreDropsLegacyScrollRootTable(t *testing.T) {
	stateDir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE scrolls (
			id TEXT PRIMARY KEY,
			artifact TEXT NOT NULL,
			scroll_root TEXT NOT NULL,
			data_root TEXT NOT NULL,
			scroll_name TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		INSERT INTO scrolls (id, artifact, scroll_root, data_root, scroll_name, status, created_at, updated_at)
		VALUES ('old', 'artifact', 'docker-volume://old-root', 'docker-volume://old-data', 'old-scroll', 'created', '2026-05-16T00:00:00Z', '2026-05-16T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	store, err := NewStateStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetScroll("old"); err == nil {
		t.Fatal("legacy scroll survived schema reset")
	}
	if err := store.CreateScroll(&domain.RuntimeScroll{
		ID:         "new",
		Artifact:   "artifact",
		Root:       "docker-volume://new-root",
		ScrollName: "new-scroll",
		Status:     domain.RuntimeScrollStatusCreated,
	}); err != nil {
		t.Fatal(err)
	}
}
