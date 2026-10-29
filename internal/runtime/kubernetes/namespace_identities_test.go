package kubernetes

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/workloadidentity"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCommandIdentityFollowsRuntimeNamespaceAndSurvivesReattachment(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := fake.NewSimpleClientset()
	registry, err := newNamespaceIdentities(ctx, client, "daemon")
	if err != nil {
		t.Fatal(err)
	}
	mount, err := registry.Ensure(ctx, workloadidentity.Record{RuntimeRoot: "k8s://deployment/data", ExecutionID: "execution", DeploymentID: "deployment", Command: "agent", Permissions: []string{"logs:read"}})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := client.CoreV1().Secrets("deployment").Get(ctx, mount.Secret, metav1.GetOptions{})
	if err != nil {
		t.Fatal("credential not mounted in workload namespace", err)
	}
	if secrets, _ := client.CoreV1().Secrets("daemon").List(ctx, metav1.ListOptions{}); len(secrets.Items) != 0 {
		t.Fatal("credential placed in daemon namespace")
	}
	reopened, err := newNamespaceIdentities(ctx, client, "daemon")
	if err != nil {
		t.Fatal(err)
	}
	token := string(secret.Data["token"])
	if record, err := reopened.Lookup(ctx, token); err != nil || record.DeploymentID != "deployment" {
		t.Fatal("namespace registry not recovered", err)
	}
	if err := reopened.Revoke(ctx, "execution"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Ensure(ctx, workloadidentity.Record{RuntimeRoot: "k8s://new-namespace/data", ExecutionID: "execution", DeploymentID: "deployment", Command: "agent"}); !errors.Is(err, workloadidentity.ErrUnauthorized) {
		t.Fatal("late provisioning in a new namespace resurrected a revoked execution")
	}
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := registry.Lookup(ctx, token); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cross-daemon revocation did not propagate")
		}
		time.Sleep(time.Millisecond)
	}
}
