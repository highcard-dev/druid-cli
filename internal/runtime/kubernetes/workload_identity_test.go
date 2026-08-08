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

func TestAuthenticateWorkloadRequiresBoundDruidDevPod(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "dev-0", Namespace: "games", UID: types.UID("pod-uid"), Labels: map[string]string{
			labelManagedBy: "druid", labelProcedure: "dev", labelRuntimeID: "runtime-a",
		}},
		Spec: corev1.PodSpec{ServiceAccountName: runtimeDevServiceAccount},
	})
	client.PrependReactor("create", "tokenreviews", tokenReviewReactor(runtimeDevServiceAccount, "games", "dev-0", "pod-uid", true))
	backend := NewWithClient(Config{Namespace: "games", OperatorServiceAccount: "operator-system/operator"}, nil, client)
	identity, err := backend.AuthenticateWorkload(context.Background(), "token")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Kind != "dev" || identity.RuntimeID != "runtime-a" || identity.Namespace != "games" {
		t.Fatalf("identity = %#v", identity)
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
