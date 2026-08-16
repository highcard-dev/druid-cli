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
