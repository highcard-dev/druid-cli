package services_test

import (
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services"
)

func TestRuntimeStateStorePersistsCommandStatuses(t *testing.T) {
	store := services.NewRuntimeStateStore(t.TempDir())
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
