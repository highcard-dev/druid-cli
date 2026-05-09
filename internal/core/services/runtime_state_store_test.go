package services_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services"
	_ "modernc.org/sqlite"
)

func TestRuntimeStateStorePersistsCommandStatuses(t *testing.T) {
	store := services.NewRuntimeStateStore(t.TempDir())
	exitCode := 2
	scroll := &domain.RuntimeScroll{
		ID:         "test",
		Artifact:   "example",
		ScrollRoot: "/tmp/spec",
		DataRoot:   "/tmp/data",
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

func TestRuntimeStateStoreMigratesRuntimeColumn(t *testing.T) {
	stateDir := t.TempDir()
	dbPath := filepath.Join(stateDir, "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE scrolls (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL DEFAULT '',
			artifact TEXT NOT NULL,
			runtime TEXT NOT NULL,
			scroll_root TEXT NOT NULL,
			data_root TEXT NOT NULL DEFAULT '',
			scroll_name TEXT NOT NULL,
			scroll_yaml TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			commands_json TEXT NOT NULL DEFAULT '{}'
		)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO scrolls (id, owner_id, artifact, runtime, scroll_root, data_root, scroll_name, scroll_yaml, status, created_at, updated_at, commands_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "legacy", "", "example", "docker", "/tmp/spec", "/tmp/data", "legacy", "name: legacy\n", "stopped", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "{}"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store := services.NewRuntimeStateStore(stateDir)
	got, err := store.GetScroll("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "legacy" || got.Artifact != "example" || got.ScrollYAML != "name: legacy\n" {
		t.Fatalf("migrated scroll = %#v", got)
	}

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`PRAGMA table_info(scrolls)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "runtime" {
			t.Fatal("runtime column should be removed during migration")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
