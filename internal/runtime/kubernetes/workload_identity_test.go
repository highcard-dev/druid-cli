package kubernetes

import (
	"context"
	"testing"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestAuthenticateWorkloadRejectsUnrecognizedServiceAccount(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "other-0", Namespace: "games", UID: types.UID("pod-uid"), Labels: map[string]string{
			labelManagedBy: "druid", labelRuntimeID: "runtime-a",
		}},
		Spec: corev1.PodSpec{ServiceAccountName: "unrecognized"},
	})
	client.PrependReactor("create", "tokenreviews", tokenReviewReactor("unrecognized", "games", "other-0", "pod-uid", true))
	backend := NewWithClient(Config{Namespace: "games", OperatorServiceAccount: "operator-system/operator"}, nil, client)
	if _, err := backend.AuthenticateWorkload(context.Background(), "token"); err == nil {
		t.Fatal("unrecognized service account pod was accepted")
	}
}

func TestAuthenticateWorkloadRejectsWrongAudienceOrDeletedPod(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", tokenReviewReactor(runtimeWorkerServiceAccount, "games", "worker-0", "pod-uid", false))
	backend := NewWithClient(Config{Namespace: "games", OperatorServiceAccount: "operator-system/operator"}, nil, client)
	if _, err := backend.AuthenticateWorkload(context.Background(), "token"); err == nil {
		t.Fatal("unauthenticated token was accepted")
	}
	client = k8sfake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", tokenReviewReactor(runtimeWorkerServiceAccount, "games", "worker-0", "pod-uid", true))
	backend = NewWithClient(Config{Namespace: "games", OperatorServiceAccount: "operator-system/operator"}, nil, client)
	if _, err := backend.AuthenticateWorkload(context.Background(), "token"); err == nil {
		t.Fatal("deleted pod token was accepted")
	}
}

func TestAuthenticateWorkloadRejectsCrossRuntimeLabel(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-0", Namespace: "games", UID: types.UID("pod-uid"), Labels: map[string]string{
			labelManagedBy: "druid", labelComponent: "other", labelRuntimeID: "runtime-b",
		}},
		Spec: corev1.PodSpec{ServiceAccountName: runtimeWorkerServiceAccount},
	})
	client.PrependReactor("create", "tokenreviews", tokenReviewReactor(runtimeWorkerServiceAccount, "games", "worker-0", "pod-uid", true))
	backend := NewWithClient(Config{Namespace: "games", OperatorServiceAccount: "operator-system/operator"}, nil, client)
	if _, err := backend.AuthenticateWorkload(context.Background(), "token"); err == nil {
		t.Fatal("worker without worker-pull label was accepted")
	}
}

func TestEnsureRuntimeServiceAccountsCreatesBothAndIsIdempotent(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "games"}, nil, client)

	if err := backend.ensureRuntimeServiceAccounts(context.Background(), "games"); err != nil {
		t.Fatal(err)
	}
	if err := backend.ensureRuntimeServiceAccounts(context.Background(), "games"); err != nil {
		t.Fatal(err)
	}
	accounts, err := client.CoreV1().ServiceAccounts("games").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts.Items) != 1 {
		t.Fatalf("service accounts = %#v, want one", accounts.Items)
	}
	for _, account := range accounts.Items {
		if account.Labels[labelManagedBy] != "druid" || account.Labels[labelComponent] != "workload-identity" {
			t.Fatalf("service account labels = %#v", account.Labels)
		}
		if account.AutomountServiceAccountToken == nil || *account.AutomountServiceAccountToken {
			t.Fatalf("service account automount = %#v, want false", account.AutomountServiceAccountToken)
		}
	}
}

func tokenReviewReactor(serviceAccount, namespace, podName, podUID string, authenticated bool) k8stesting.ReactionFunc {
	return func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.TokenReview{Status: authv1.TokenReviewStatus{
			Authenticated: authenticated,
			Audiences:     []string{"druid-cli"},
			User: authv1.UserInfo{Username: "system:serviceaccount:" + namespace + ":" + serviceAccount, Extra: map[string]authv1.ExtraValue{
				podNameExtra: {podName}, podUIDExtra: {podUID},
			}},
		}}, nil
	}
}
