package services_test

import (
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/core/services"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

func TestProcedureLauncherPassesCommandContextToRuntimeBackend(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	runtimeBackend := mock_ports.NewMockRuntimeBackendInterface(ctrl)
	command := &domain.CommandInstructionSet{
		Run: domain.RunModePersistent,
		Procedures: []*domain.Procedure{{
			Image: "alpine:3.20",
		}},
	}
	file := &domain.File{Ports: []domain.Port{{Name: "http", Port: 80}}}

	scrollService.EXPECT().GetCommand("serve").Return(command, nil)
	scrollService.EXPECT().GetFile().Return(file)
	runtimeBackend.EXPECT().Name().Return("docker")
	runtimeBackend.EXPECT().RunCommand(gomock.Any()).DoAndReturn(func(runtimeCommand ports.RuntimeCommand) (*int, error) {
		if runtimeCommand.Name != "serve" {
			t.Fatalf("Name = %s, want serve", runtimeCommand.Name)
		}
		if runtimeCommand.Command != command {
			t.Fatal("Command was not forwarded to runtime backend")
		}
		if runtimeCommand.Root != "/runtime-data" {
			t.Fatalf("Root = %s, want /runtime-data", runtimeCommand.Root)
		}
		if len(runtimeCommand.GlobalPorts) != 1 || runtimeCommand.GlobalPorts[0].Name != "http" {
			t.Fatalf("GlobalPorts = %#v", runtimeCommand.GlobalPorts)
		}
		return nil, nil
	})

	launcher, err := services.NewProcedureLauncher(scrollService, runtimeBackend, "/runtime-data")
	if err != nil {
		t.Fatal(err)
	}
	if err := launcher.Run("serve"); err != nil {
		t.Fatal(err)
	}
}

func TestProcedureLauncherPassesScrollIDToRuntimeBackend(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	runtimeBackend := mock_ports.NewMockRuntimeBackendInterface(ctrl)
	command := &domain.CommandInstructionSet{
		Procedures: []*domain.Procedure{{Image: "alpine:3.20"}},
	}
	scrollService.EXPECT().GetCommand("serve").Return(command, nil)
	scrollService.EXPECT().GetFile().Return(&domain.File{})
	runtimeBackend.EXPECT().Name().Return("docker")
	runtimeBackend.EXPECT().RunCommand(gomock.Any()).DoAndReturn(func(runtimeCommand ports.RuntimeCommand) (*int, error) {
		if runtimeCommand.ScrollID != "scroll-a" {
			t.Fatalf("ScrollID = %s, want scroll-a", runtimeCommand.ScrollID)
		}
		return nil, nil
	})

	launcher, err := services.NewProcedureLauncherForScroll(scrollService, runtimeBackend, "/runtime-data", "scroll-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := launcher.Run("serve"); err != nil {
		t.Fatal(err)
	}
}

func TestProcedureLauncherBuildsStableRuntimeEnv(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	runtimeBackend := mock_ports.NewMockRuntimeBackendInterface(ctrl)
	command := &domain.CommandInstructionSet{
		Procedures: []*domain.Procedure{{
			Id:    ptrString("web"),
			Image: "alpine:3.20",
			Env: map[string]string{
				"APP_ENV":           "test",
				"DRUID_PORT_HTTP":   "user-value",
				"DRUID_SCROLL_NAME": "user-name",
			},
		}},
	}
	file := &domain.File{
		Name: "scroll-name",
		Ports: []domain.Port{{
			Name:     "http",
			Port:     8080,
			Protocol: "http",
		}},
	}

	scrollService.EXPECT().GetCommand("serve").Return(command, nil)
	scrollService.EXPECT().GetFile().Return(file)
	runtimeBackend.EXPECT().Name().Return("kubernetes")
	runtimeBackend.EXPECT().RunCommand(gomock.Any()).DoAndReturn(func(runtimeCommand ports.RuntimeCommand) (*int, error) {
		env := runtimeCommand.ProcedureEnv["web"]
		if env["APP_ENV"] != "test" {
			t.Fatalf("APP_ENV = %q, want test", env["APP_ENV"])
		}
		if env["DRUID_PORT_HTTP"] != "8080" || env["DRUID_PORT_HTTP_1"] != "8080" {
			t.Fatalf("port env = %#v", env)
		}
		if env["DRUID_SCROLL_ID"] != "scroll-a" || env["DRUID_SCROLL_NAME"] != "scroll-name" {
			t.Fatalf("scroll env = %#v", env)
		}
		if env["DRUID_RUNTIME_BACKEND"] != "kubernetes" {
			t.Fatalf("backend env = %#v", env)
		}
		if env["DRUID_PORT_HTTP_PUBLIC"] != "443" || env["DRUID_PORT_HTTP_HOST"] != "web.example.test" || env["DRUID_PORT_HTTP_URL"] != "https://web.example.test" {
			t.Fatalf("routing env = %#v", env)
		}
		if env["DRUID_IP"] != "192.0.2.10" || env["DRUID_PORT_HTTP_IP"] != "192.0.2.10" {
			t.Fatalf("ip env = %#v", env)
		}
		if _, ok := env["DRUID_IP_WAIT"]; ok {
			t.Fatalf("DRUID_IP_WAIT should not be set after routing: %#v", env)
		}
		return nil, nil
	})

	launcher, err := services.NewProcedureLauncherForRuntime(scrollService, runtimeBackend, "/runtime-data", "scroll-a", "", func() []domain.RuntimeRouteAssignment {
		return []domain.RuntimeRouteAssignment{{
			Name:       "web-http",
			PortName:   "http",
			Host:       "web.example.test",
			ExternalIP: "192.0.2.10",
			PublicPort: 443,
			URL:        "https://web.example.test",
		}}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := launcher.Run("serve"); err != nil {
		t.Fatal(err)
	}
}

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

func ptrString(value string) *string {
	return &value
}
