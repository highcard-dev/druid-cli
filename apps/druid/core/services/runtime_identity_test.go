package services

import (
	"context"
	"errors"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
)

// A Pod-authenticated identity supplies binding, not permission grants.
type podIdentityBackend struct {
	*toolTestBackend
	record workloadidentity.Record
}

func (b *podIdentityBackend) AuthenticateCommandIdentity(context.Context, string) (workloadidentity.Record, error) {
	return b.record, nil
}

func TestPodIdentityUsesCurrentDefinitionAndExecution(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*RuntimeSession, *workloadidentity.Record)
	}{
		{name: "active"},
		{name: "replaced", mutate: func(s *RuntimeSession, _ *workloadidentity.Record) {
			s.runtimeScroll.Procedures["agent"]["agent-console"] = domain.LockStatus{ExecutionID: "replacement", Status: domain.ScrollLockStatusRunning}
		}},
		{name: "completed", mutate: func(s *RuntimeSession, _ *workloadidentity.Record) {
			s.runtimeScroll.Procedures["agent"]["agent-console"] = domain.LockStatus{ExecutionID: "agent-execution", Status: domain.ScrollLockStatusDone}
		}},
		{name: "failed", mutate: func(s *RuntimeSession, _ *workloadidentity.Record) {
			s.runtimeScroll.Procedures["agent"]["agent-console"] = domain.LockStatus{ExecutionID: "agent-execution", Status: domain.ScrollLockStatusError}
		}},
		{name: "removed", mutate: func(s *RuntimeSession, _ *workloadidentity.Record) {
			delete(s.scrollService.GetFile().Commands, "agent")
		}},
		{name: "opted out", mutate: func(s *RuntimeSession, _ *workloadidentity.Record) {
			s.scrollService.GetFile().Commands["agent"].Identity = nil
		}},
		{name: "stopped", mutate: func(s *RuntimeSession, _ *workloadidentity.Record) {
			s.runtimeScroll.Status = domain.RuntimeScrollStatusStopped
		}},
		{name: "deleted", mutate: func(s *RuntimeSession, _ *workloadidentity.Record) {
			s.runtimeScroll.Status = domain.RuntimeScrollStatusDeleted
		}},
		{name: "other namespace or volume", mutate: func(_ *RuntimeSession, r *workloadidentity.Record) { r.RuntimeRoot = "k8s://other/data" }},
		{name: "other deployment", mutate: func(_ *RuntimeSession, r *workloadidentity.Record) { r.DeploymentID = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			supervisor, backend, _ := commandToolsFixture(t, "commands:list", "queue:read")
			session := supervisor.sessions["tools"]
			record := workloadidentity.Record{RuntimeRoot: session.runtimeScroll.Root, DeploymentID: "tools", Command: "agent", ExecutionID: "agent-execution"}
			if test.mutate != nil {
				test.mutate(session, &record)
			}
			supervisor.runtimeBackend = &podIdentityBackend{toolTestBackend: backend, record: record}
			authenticated, err := supervisor.AuthenticateCommand(context.Background(), "pod-bound-token", "tools")
			if test.name == "active" {
				if err != nil || !authenticated.Allows("commands:list") || !authenticated.Allows("queue:read") || authenticated.Allows("commands:run") {
					t.Fatalf("identity=%+v err=%v", authenticated, err)
				}
			} else if !errors.Is(err, workloadidentity.ErrUnauthorized) {
				t.Fatalf("accepted %s: %v", test.name, err)
			}
		})
	}
}
