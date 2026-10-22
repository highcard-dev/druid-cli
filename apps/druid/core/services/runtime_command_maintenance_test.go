package services

import (
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

func TestCommandAdmissionWaitsForMaintenance(t *testing.T) {
	for _, sync := range []bool{false, true} {
		t.Run(map[bool]string{false: "run", true: "run-and-wait"}[sync], func(t *testing.T) {
			store := newTestStateStore(t)
			fixture := &domain.RuntimeScroll{ID: "command-maintenance", Root: "runtime://fixture", ScrollYAML: cachedScrollYAML("start"), Status: domain.RuntimeScrollStatusStopped}
			if err := store.CreateScroll(fixture); err != nil {
				t.Fatal(err)
			}
			supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), &fakeWorkerBackend{})
			unlock := supervisor.lockRuntimeOperation(fixture.ID)
			defer func() {
				if unlock != nil {
					unlock()
				}
			}()
			done := make(chan error, 1)
			go func() {
				var err error
				if sync {
					_, err = supervisor.RunAndWait(fixture.ID, "missing")
				} else {
					_, err = supervisor.Run(fixture.ID, "missing")
				}
				done <- err
			}()
			select {
			case err := <-done:
				t.Fatalf("command reached session during maintenance: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			unlock()
			unlock = nil
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("missing command accepted")
				}
			case <-time.After(time.Second):
				t.Fatal("command admission did not resume")
			}
		})
	}
}

func TestCommandWaitDoesNotHoldMaintenanceLock(t *testing.T) {
	store := newTestStateStore(t)
	fixture := &domain.RuntimeScroll{ID: "command-wait", Root: "runtime://fixture", ScrollYAML: cachedScrollYAML("start"), Status: domain.RuntimeScrollStatusStopped}
	if err := store.CreateScroll(fixture); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	backend := &fakeWorkerBackend{runCommand: func(ports.RuntimeCommand) (*int, error) {
		close(started)
		<-release
		code := 0
		return &code, nil
	}}
	supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
	done := make(chan error, 1)
	go func() { _, err := supervisor.RunAndWait(fixture.ID, "start"); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("command did not start")
	}
	acquired := make(chan struct{})
	go func() { unlock := supervisor.lockRuntimeOperation(fixture.ID); close(acquired); unlock() }()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("command wait blocks maintenance admission")
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("command waiter did not complete")
	}
}
