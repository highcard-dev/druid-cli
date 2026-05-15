package docker

import (
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func TestContainerNameUsesDeploymentCommandAndProcedureIndex(t *testing.T) {
	root := "docker-volume://druid-0636a354-b3f4-4471-8749-3e17ab6c52-data"

	got := ContainerName(root, procedureResourceName("start", 0))
	want := "0636a354-b3f4-4471-8749-3e17ab6c52-start-0"
	if got != want {
		t.Fatalf("container name = %q, want %q", got, want)
	}
}

func TestRoutingTargetsUseFirstConcreteProcedureForSharedDockerPort(t *testing.T) {
	root := "docker-volume://druid-minecraft-data"
	coldstart := "coldstart"
	start := "start"

	targets, err := (&Backend{}).RoutingTargets(root, map[string]*domain.CommandInstructionSet{
		"start": {Procedures: []*domain.Procedure{
			{Id: &coldstart, ExpectedPorts: []domain.ExpectedPort{{Name: "main"}}},
			{Id: &start, ExpectedPorts: []domain.ExpectedPort{{Name: "main"}}},
		}},
	}, []domain.Port{{Name: "main", Port: 25565, Protocol: "tcp"}})
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range targets {
		if target.Name != "main" {
			continue
		}
		if target.Procedure != "coldstart" || target.ServiceName != ContainerName(root, "start-0") {
			t.Fatalf("target = %#v", target)
		}
		return
	}
	t.Fatalf("main target missing: %#v", targets)
}
