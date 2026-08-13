package services

import (
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func TestMergeRuntimePortsAddsReservations(t *testing.T) {
	ports, err := mergeRuntimePorts(
		[]domain.Port{{Name: "main", Port: 27015, Protocol: "udp"}},
		[]domain.Port{{Name: "ssh", Port: 2222, Protocol: "tcp"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 2 || ports[1].Name != "ssh" || ports[1].Port != 2222 {
		t.Fatalf("merged ports = %#v", ports)
	}
}

func TestMergeRuntimePortsRejectsScrollConflict(t *testing.T) {
	_, err := mergeRuntimePorts(
		[]domain.Port{{Name: "ssh", Port: 22, Protocol: "tcp"}},
		[]domain.Port{{Name: "ssh", Port: 2222, Protocol: "tcp"}},
	)
	if err == nil {
		t.Fatal("expected conflicting port error")
	}
}

func TestValidateReservedPortsRejectsUnsafeIdentifiers(t *testing.T) {
	err := validateReservedPorts([]domain.Port{{Name: "../ssh", Port: 2222, Protocol: "tcp"}})
	if err == nil {
		t.Fatal("expected unsafe identifier error")
	}
}

func TestSetReservedPortsRepairsStateBeforeSessionValidation(t *testing.T) {
	store := newTestStateStore(t)
	scroll := &domain.RuntimeScroll{
		ID:         "reserved-port-repair",
		Artifact:   "local",
		Root:       "/tmp/reserved-port-repair",
		ScrollName: "reserved-port-repair",
		ScrollYAML: `name: reserved-port-repair
desc: Reserved port repair
version: 0.1.0
app_version: "1.0"
commands:
  ssh:
    run: persistent
    procedures:
      - id: ssh
        image: alpine:3.20
        command: ["sleep", "infinity"]
        expectedPorts:
          - name: ssh
`,
		Status:     domain.RuntimeScrollStatusCreated,
		Procedures: domain.ProcedureStatusMap{},
	}
	if err := store.CreateScroll(scroll); err != nil {
		t.Fatal(err)
	}

	supervisor := NewRuntimeSupervisor(store, nil, &fakeWorkerBackend{})
	if _, err := supervisor.sessionFor(scroll.ID); err == nil {
		t.Fatal("expected session validation to fail before the port is reserved")
	}
	reservation := domain.Port{Name: "ssh", Port: 2222, Protocol: "tcp"}
	if err := supervisor.SetReservedPorts(scroll.ID, []domain.Port{reservation}); err != nil {
		t.Fatal(err)
	}
	session, err := supervisor.sessionFor(scroll.ID)
	if err != nil {
		t.Fatalf("session validation after reservation: %v", err)
	}
	if len(session.scrollService.GetFile().Ports) != 0 {
		t.Fatalf("reserved ports leaked into persisted scroll ports: %#v", session.scrollService.GetFile().Ports)
	}
	got, err := store.GetScroll(scroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ReservedPorts) != 1 || got.ReservedPorts[0] != reservation {
		t.Fatalf("reserved ports = %#v, want %#v", got.ReservedPorts, reservation)
	}
}
