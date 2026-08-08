package kubernetes

import (
	"context"
	"fmt"
	"strings"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/highcard-dev/daemon/internal/core/ports"
)

const (
	podNameExtra = "authentication.kubernetes.io/pod-name"
	podUIDExtra  = "authentication.kubernetes.io/pod-uid"
)

// AuthenticateWorkload verifies the bound ServiceAccount token with the API
// server and then re-checks the pod that owns it. The pod lookup makes a token
// invalid as soon as its bound workload disappears.
func (b *Backend) AuthenticateWorkload(ctx context.Context, token string) (ports.RuntimeWorkloadIdentity, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("missing workload bearer token")
	}
	review, err := b.client.AuthenticationV1().TokenReviews().Create(ctx, &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{Token: token, Audiences: []string{b.config.ServiceAccountAudience}},
	}, metav1.CreateOptions{})
	if err != nil {
		return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("review workload token: %w", err)
	}
	if !review.Status.Authenticated || len(review.Status.Audiences) == 0 {
		return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("workload token is not authenticated for audience %q", b.config.ServiceAccountAudience)
	}
	namespace, serviceAccount, ok := parseServiceAccountSubject(review.Status.User.Username)
	if !ok {
		return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("workload token is not a service account")
	}
	podName := userExtra(review.Status.User.Extra, podNameExtra)
	podUID := userExtra(review.Status.User.Extra, podUIDExtra)
	if podName == "" || podUID == "" {
		return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("workload token is not bound to a pod")
	}
	pod, err := b.client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("bound pod no longer exists")
		}
		return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("get bound pod: %w", err)
	}
	if string(pod.UID) != podUID || pod.Spec.ServiceAccountName != serviceAccount {
		return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("workload token does not match live pod")
	}
	identity := ports.RuntimeWorkloadIdentity{
		Namespace: namespace, ServiceAccount: serviceAccount, PodName: podName, PodUID: podUID,
		RuntimeID: pod.Labels[labelRuntimeID],
	}
	switch serviceAccount {
	case runtimeDevServiceAccount:
		if pod.Labels[labelManagedBy] != "druid" || pod.Labels[labelProcedure] != "dev" || identity.RuntimeID == "" {
			return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("invalid Druid dev workload")
		}
		identity.Kind = "dev"
	case runtimeWorkerServiceAccount:
		if pod.Labels[labelManagedBy] != "druid" || pod.Labels[labelComponent] != "worker-pull" || identity.RuntimeID == "" {
			return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("invalid Druid worker workload")
		}
		identity.Kind = "worker"
	default:
		if !b.isOperatorServiceAccount(namespace, serviceAccount) {
			return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("service account is not permitted")
		}
		identity.Kind = "operator"
	}
	return identity, nil
}

func (b *Backend) ensureRuntimeServiceAccount(ctx context.Context, namespace string, name string) error {
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{
			labelManagedBy: "druid", labelComponent: "workload-identity",
		}},
		AutomountServiceAccountToken: ptrBool(false),
	}
	_, err := b.client.CoreV1().ServiceAccounts(namespace).Create(ctx, serviceAccount, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (b *Backend) isOperatorServiceAccount(namespace string, serviceAccount string) bool {
	expected := strings.TrimSpace(b.config.OperatorServiceAccount)
	if expected == "" {
		return false
	}
	return expected == namespace+"/"+serviceAccount
}

func parseServiceAccountSubject(subject string) (string, string, bool) {
	parts := strings.Split(subject, ":")
	if len(parts) != 4 || parts[0] != "system" || parts[1] != "serviceaccount" || parts[2] == "" || parts[3] == "" {
		return "", "", false
	}
	return parts[2], parts[3], true
}

func userExtra(extra map[string]authv1.ExtraValue, key string) string {
	values := extra[key]
	if len(values) != 1 {
		return ""
	}
	return values[0]
}
