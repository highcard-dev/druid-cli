package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

func TestBackupUnknownFailureDoesNotRestart(t *testing.T) {
	store := newTestStateStore(t)
	runtime := &domain.RuntimeScroll{
		ID: "backup-unknown", Artifact: "registry.local/original:1", Root: "runtime://backup-unknown",
		ScrollName: "original", ScrollYAML: strings.Replace(cachedScrollYAML("start"), "run: once", "run: persistent", 1),
		Status: domain.RuntimeScrollStatusRunning, Procedures: domain.ProcedureStatusMap{},
	}
	if err := store.CreateScroll(runtime); err != nil {
		t.Fatal(err)
	}
	backend := &fakeWorkerBackend{backupErr: errors.New("backup worker connection lost"),
		procedureStatusUpdates: []ports.ProcedureStatusUpdate{{Procedure: "start.0", Status: domain.ScrollLockStatusRunning}}}
	supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
	if _, err := supervisor.Backup(runtime.ID, "registry.local/backups:1", nil); err == nil {
		t.Fatal("expected backup error")
	}
	stored, err := store.GetScroll(runtime.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.RuntimeScrollStatusError || stored.LastError == "" {
		t.Fatalf("unconfirmed backup termination must remain stopped with an error; status=%s error=%q", stored.Status, stored.LastError)
	}
}
