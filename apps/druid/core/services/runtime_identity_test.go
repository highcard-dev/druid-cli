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
}

func (b podIdentityBackend) AuthenticateCommandIdentity(context.Context, string) (workloadidentity.Record, error) {
	return b.record, nil
}

func (b podIdentityBackend) PrepareCommandIdentity(context.Context, workloadidentity.Record) (workloadidentity.CredentialMount, error) {
	return workloadidentity.CredentialMount{}, nil
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
