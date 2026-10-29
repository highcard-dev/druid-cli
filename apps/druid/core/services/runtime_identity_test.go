package services

import (
	"context"
	"errors"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
)

type podIdentityBackend struct {
	ports.RuntimeBackendInterface
	record workloadidentity.Record
	name   string
}

func (b podIdentityBackend) AuthenticateCommandIdentity(context.Context, string) (workloadidentity.Record, error) {
	return b.record, nil
}

func (b podIdentityBackend) PrepareCommandIdentity(context.Context, workloadidentity.Record) error {
	return nil
}
func (b podIdentityBackend) Name() string {
	if b.name != "" {
		return b.name
	}
	return "fixture"
}

func TestCommandAuthenticationNeedsNoDaemonState(t *testing.T) {
	backend := podIdentityBackend{record: workloadidentity.Record{DeploymentID: "deployment", Permissions: []string{"commands:list"}}}
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
