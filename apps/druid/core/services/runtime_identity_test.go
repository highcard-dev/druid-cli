package services

import (
	"context"
	"errors"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
)

func TestHydratedIdentityCommandUsesExecutionStatusWithoutRecoveryFlags(t *testing.T) {
	for _, execution := range []string{"existing-run", ""} {
		t.Run("execution="+execution, func(t *testing.T) {
			supervisor, backend, _ := commandToolsFixture(t)
			session := supervisor.sessions["tools"]
			session.workloadAPIURL = "http://runtime.local"
			session.runtimeScroll.Procedures["agent"]["agent-console"] = domain.LockStatus{ExecutionID: execution, Status: domain.ScrollLockStatusRunning}
			if err := session.HydrateFromState(session.Snapshot()); err != nil {
				t.Fatal(err)
			}
			var seen ports.RuntimeCommand
			backend.runCommand = func(command ports.RuntimeCommand) (*int, error) { seen = command; return nil, nil }
			if err := session.runCommand("agent"); err != nil {
				t.Fatal(err)
			}
			if seen.ExecutionID == "" || (execution != "" && seen.ExecutionID != execution) || !seen.Command.Procedures[0].RuntimeIdentity.Enabled {
				t.Fatal("hydration lost execution identity or token projection")
			}
			if _, present := seen.ProcedureEnv["agent-console"]["DRUID_EXECUTION_ID"]; present {
				t.Fatal("unused execution metadata leaked into workload environment")
			}
		})
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

func (b podIdentityBackend) PrepareCommandIdentity(context.Context, string) error {
	return nil
}
func (b podIdentityBackend) Name() string {
	if b.name != "" {
		return b.name
	}
	return "fixture"
}

func TestCommandAuthenticationNeedsNoDaemonState(t *testing.T) {
	backend := podIdentityBackend{record: workloadidentity.Identity{DeploymentID: "deployment", Permissions: []string{"commands:list"}}}
	supervisor, err := NewRuntimeSupervisor(nil, nil, ports.RuntimeBackendFactoryFunc(func(ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) { return backend, nil }))
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
	supervisor, err := NewRuntimeSupervisor(nil, nil, ports.RuntimeBackendFactoryFunc(func(ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) { return backend, nil }))
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
