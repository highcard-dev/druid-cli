package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
)

func TestHydratedIdentityCommandPassesConfigurationToBackend(t *testing.T) {
	supervisor, backend, _ := commandToolsFixture(t)
	session := supervisor.sessions["tools"]
	session.workloadAPIURL = "http://runtime.local"
	session.runtimeScroll.Procedures["agent"]["agent-console"] = domain.LockStatus{Status: domain.ScrollLockStatusRunning}
	if err := session.HydrateFromState(session.Snapshot()); err != nil {
		t.Fatal(err)
	}
	var seen ports.RuntimeCommand
	backend.runCommand = func(command ports.RuntimeCommand) (*int, error) { seen = command; return nil, nil }
	if err := session.runCommand("agent"); err != nil {
		t.Fatal(err)
	}
	if seen.Command.Identity == nil || seen.WorkloadAPIURL != "http://runtime.local" || seen.Command.Procedures[0].RuntimeIdentity != nil {
		t.Fatal("session must pass identity configuration without preparing credentials")
	}
}

type podIdentityBackend struct {
	ports.RuntimeBackendInterface
	record workloadidentity.Identity
	name   string
}

func (b podIdentityBackend) AuthenticateCommandIdentity(context.Context, string) (workloadidentity.Identity, error) {
	return b.record, nil
}

func (b podIdentityBackend) Name() string {
	if b.name != "" {
		return b.name
	}
	return "fixture"
}

func TestSupervisorConfiguresWorkloadURLDuringConstruction(t *testing.T) {
	for _, base := range []string{"", "http://runtime.local/", "https://runtime.local/prefix///"} {
		supervisor, err := NewRuntimeSupervisor(nil, nil, ports.RuntimeBackendFactoryFunc(func(observer ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
			// The backend receives an already configured supervisor.
			if observer.(*RuntimeSupervisor).workloadAPIURL != strings.TrimRight(base, "/") {
				t.Fatal("workload URL was not configured before backend creation")
			}
			return podIdentityBackend{}, nil
		}), base)
		if err != nil || supervisor.workloadAPIURL != strings.TrimRight(base, "/") {
			t.Fatalf("base=%q supervisor=%+v err=%v", base, supervisor, err)
		}
	}
	for _, base := range []string{"runtime.local", "ftp://runtime.local", "https://user:password@runtime.local", "https://runtime.local/?token=value", "https://runtime.local/#fragment", "http://%"} {
		t.Run(base, func(t *testing.T) {
			factory := ports.RuntimeBackendFactoryFunc(func(ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
				t.Fatal("invalid configuration reached backend creation")
				return nil, nil
			})
			if _, err := NewRuntimeSupervisor(nil, nil, factory, base); err == nil {
				t.Fatal("invalid workload URL accepted")
			}
		})
	}
}

func TestCommandAuthenticationNeedsNoDaemonState(t *testing.T) {
	backend := podIdentityBackend{record: workloadidentity.Identity{DeploymentID: "deployment", Permissions: []string{"commands:list"}}}
	supervisor, err := NewRuntimeSupervisor(nil, nil, ports.RuntimeBackendFactoryFunc(func(ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) { return backend, nil }), "")
	if err != nil {
		t.Fatal(err)
	}
	record, err := supervisor.AuthenticateCommand(context.Background(), "verified-pod-token", "deployment")
	if err != nil || !record.Allows("commands:list") || record.Allows("commands:run") {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	if _, err := supervisor.AuthenticateCommand(context.Background(), "verified-pod-token", "other"); !errors.Is(err, workloadidentity.ErrUnauthorized) {
		t.Fatal("cross-deployment credential accepted")
	}
	// Permission denial must also happen without touching runtime storage.
	if _, err := supervisor.CallCommandTool(context.Background(), "verified-pod-token", "deployment", "run_command", ToolInput{Command: "example"}); err == nil {
		t.Fatal("ungranted permission accepted")
	}
}

func TestDockerToolsIgnoreCredentialsPermissionsAndDeploymentBinding(t *testing.T) {
	backend := podIdentityBackend{name: "docker"}
	supervisor, err := NewRuntimeSupervisor(nil, nil, ports.RuntimeBackendFactoryFunc(func(ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) { return backend, nil }), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, deployment := range []string{"one", "other"} {
		for _, token := range []string{"", "invalid-token"} {
			record, err := supervisor.AuthenticateCommand(context.Background(), token, deployment)
			if err != nil || record.DeploymentID != deployment {
				t.Fatal("Docker unexpectedly required credentials")
			}
			for _, permission := range ToolPermissions {
				if !record.Allows(permission) {
					t.Fatalf("Docker denied %s", permission)
				}
			}
		}
	}
}
