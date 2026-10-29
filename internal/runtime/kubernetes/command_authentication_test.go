package kubernetes

import (
	"context"
	"errors"
	"testing"

	"github.com/highcard-dev/daemon/internal/workloadidentity"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func agentPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-0", Namespace: "games", UID: types.UID("pod-uid"),
			Labels:      map[string]string{labelManagedBy: "druid", labelComponent: "runtime", labelScrollID: "data", labelRuntimeID: "data", labelCommand: "agent-command", workloadidentity.ExecutionLabel: "execution"},
			Annotations: map[string]string{commandDeploymentAnnotation: "full-deployment-id", labelCommand: "agent_command"}},
		Spec:   corev1.PodSpec{ServiceAccountName: commandServiceAccount, Volumes: []corev1.Volume{pvcVolume("data", "data")}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func TestCommandServiceAccountAuthentication(t *testing.T) {
	for _, test := range []struct {
		name                 string
		mutate               func(*corev1.Pod, *authv1.TokenReviewStatus)
		deleted, unavailable bool
	}{
		{name: "valid"},
		{name: "wrong audience", mutate: func(_ *corev1.Pod, r *authv1.TokenReviewStatus) { r.Audiences = []string{"druid-cli"} }},
		{name: "unauthenticated", mutate: func(_ *corev1.Pod, r *authv1.TokenReviewStatus) { r.Authenticated = false }},
		{name: "unbound token", mutate: func(_ *corev1.Pod, r *authv1.TokenReviewStatus) { r.User.Extra = nil }},
		{name: "replaced pod", mutate: func(p *corev1.Pod, _ *authv1.TokenReviewStatus) { p.UID = "replacement" }},
		{name: "different account", mutate: func(p *corev1.Pod, r *authv1.TokenReviewStatus) {
			p.Spec.ServiceAccountName = runtimeWorkerServiceAccount
			r.User.Username = "system:serviceaccount:games:" + runtimeWorkerServiceAccount
		}},
		{name: "wrong namespace", mutate: func(_ *corev1.Pod, r *authv1.TokenReviewStatus) {
			r.User.Username = "system:serviceaccount:other:" + commandServiceAccount
		}},
		{name: "unmanaged pod", mutate: func(p *corev1.Pod, _ *authv1.TokenReviewStatus) { p.Labels[labelManagedBy] = "other" }},
		{name: "worker pod", mutate: func(p *corev1.Pod, _ *authv1.TokenReviewStatus) { p.Labels[labelComponent] = "worker-pull" }},
		{name: "missing deployment", mutate: func(p *corev1.Pod, _ *authv1.TokenReviewStatus) { delete(p.Annotations, commandDeploymentAnnotation) }},
		{name: "mismatched command", mutate: func(p *corev1.Pod, _ *authv1.TokenReviewStatus) { p.Annotations[labelCommand] = "other" }},
		{name: "missing execution", mutate: func(p *corev1.Pod, _ *authv1.TokenReviewStatus) { delete(p.Labels, workloadidentity.ExecutionLabel) }},
		{name: "wrong runtime root", mutate: func(p *corev1.Pod, _ *authv1.TokenReviewStatus) {
			p.Spec.Volumes[0].PersistentVolumeClaim.ClaimName = "other-data"
		}},
		{name: "completed", mutate: func(p *corev1.Pod, _ *authv1.TokenReviewStatus) { p.Status.Phase = corev1.PodSucceeded }},
		{name: "failed", mutate: func(p *corev1.Pod, _ *authv1.TokenReviewStatus) { p.Status.Phase = corev1.PodFailed }},
		{name: "terminating", mutate: func(p *corev1.Pod, _ *authv1.TokenReviewStatus) { now := metav1.Now(); p.DeletionTimestamp = &now }},
		{name: "deleted", deleted: true},
		{name: "API unavailable", unavailable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			pod := agentPod()
			status := authv1.TokenReviewStatus{Authenticated: true, Audiences: []string{commandTokenAudience}, User: authv1.UserInfo{Username: "system:serviceaccount:games:" + commandServiceAccount, Extra: map[string]authv1.ExtraValue{podNameExtra: {"agent-0"}, podUIDExtra: {"pod-uid"}}}}
			if test.mutate != nil {
				test.mutate(pod, &status)
			}
			client := fake.NewSimpleClientset()
			if !test.deleted {
				if _, err := client.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			client.PrependReactor("create", "tokenreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
				review := action.(ktesting.CreateAction).GetObject().(*authv1.TokenReview)
				if len(review.Spec.Audiences) != 1 || review.Spec.Audiences[0] != commandTokenAudience {
					t.Fatal("wrong requested audience")
				}
				if test.unavailable {
					return true, nil, errors.New("private API failure")
				}
				return true, &authv1.TokenReview{Status: status}, nil
			})
			backend := NewWithClient(Config{Namespace: "default"}, client)
			record, err := backend.AuthenticateCommandIdentity(context.Background(), "credential")
			if test.name == "valid" {
				if err != nil || record.DeploymentID != "full-deployment-id" || record.Command != "agent_command" || record.RuntimeRoot != ref("games", "data") || record.ExecutionID != "execution" || len(record.Permissions) != 0 {
					t.Fatalf("record=%+v err=%v", record, err)
				}
				// No cache or registry is required after daemon reconstruction.
				if _, err := NewWithClient(Config{}, client).AuthenticateCommandIdentity(context.Background(), "rotated-credential"); err != nil {
					t.Fatal(err)
				}
			} else {
				expected := workloadidentity.ErrUnauthorized
				if test.unavailable {
					expected = workloadidentity.ErrUnavailable
				}
				if !errors.Is(err, expected) {
					t.Fatalf("got %v, want %v", err, expected)
				}
			}
		})
	}
}

func TestCommandIdentityProvisioningOnlyCreatesAnUnprivilegedServiceAccount(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "default"}, client)
	for index := 0; index < 2; index++ {
		if _, err := backend.PrepareCommandIdentity(context.Background(), workloadidentity.Record{RuntimeRoot: ref("games", "data")}); err != nil {
			t.Fatal(err)
		}
	}
	for _, action := range client.Actions() {
		if action.GetResource().Resource != "serviceaccounts" || action.GetNamespace() != "games" {
			t.Fatalf("unexpected provisioning: %v", action)
		}
	}
	account, err := client.CoreV1().ServiceAccounts("games").Get(context.Background(), commandServiceAccount, metav1.GetOptions{})
	if err != nil || account.AutomountServiceAccountToken == nil || *account.AutomountServiceAccountToken {
		t.Fatalf("account=%v err=%v", account, err)
	}
}
