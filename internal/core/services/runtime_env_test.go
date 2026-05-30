package services_test

import (
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services"
)

func TestBuildRuntimeProcedureEnvSetsWaitBeforeRouting(t *testing.T) {
	command := &domain.CommandInstructionSet{
		Procedures: []*domain.Procedure{{Image: "alpine:3.20"}},
	}
	envs, err := services.BuildRuntimeProcedureEnv(&domain.File{
		Name:  "scroll-name",
		Ports: []domain.Port{{Name: "game-port", Port: 7777}},
	}, "serve", command, services.RuntimeEnvContext{ScrollID: "scroll-a", Backend: "docker"})
	if err != nil {
		t.Fatal(err)
	}
	env := envs["serve.0"]
	if env["DRUID_IP_WAIT"] != "true" {
		t.Fatalf("env = %#v, want DRUID_IP_WAIT", env)
	}
	if env["DRUID_PORT_GAME_PORT"] != "7777" {
		t.Fatalf("env = %#v, want normalized port env", env)
	}
}

func TestBuildRuntimeProcedureEnvDerivesURLFromPortProtocol(t *testing.T) {
	command := &domain.CommandInstructionSet{Procedures: []*domain.Procedure{{Image: "alpine:3.20"}}}
	envs, err := services.BuildRuntimeProcedureEnv(&domain.File{
		Name:  "test",
		Ports: []domain.Port{{Name: "http", Port: 8080, Protocol: "http"}},
	}, "serve", command, services.RuntimeEnvContext{
		Routing: []domain.RuntimeRouteAssignment{{
			Name:       "web-http",
			PortName:   "http",
			Host:       "localhost",
			ExternalIP: "127.0.0.1",
			PublicPort: 18080,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := envs["serve.0"]["DRUID_PORT_HTTP_URL"]; got != "http://localhost:18080" {
		t.Fatalf("DRUID_PORT_HTTP_URL = %q", got)
	}
}

func TestBuildRuntimeProcedureEnvRejectsDuplicateNormalizedPortNames(t *testing.T) {
	_, err := services.BuildRuntimeProcedureEnv(&domain.File{
		Name: "scroll-name",
		Ports: []domain.Port{
			{Name: "web-port", Port: 8080},
			{Name: "web_port", Port: 8081},
		},
	}, "serve", &domain.CommandInstructionSet{Procedures: []*domain.Procedure{{Image: "alpine:3.20"}}}, services.RuntimeEnvContext{})
	if err == nil {
		t.Fatal("expected duplicate normalized port names to fail")
	}
}
