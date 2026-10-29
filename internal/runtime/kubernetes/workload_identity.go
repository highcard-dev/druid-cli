package kubernetes

import (
	"context"
	"fmt"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	podNameExtra = "authentication.kubernetes.io/pod-name"
	podUIDExtra  = "authentication.kubernetes.io/pod-uid"
)

// Infrastructure and command identities share verification, never authorization.
func (b *Backend) authenticateBoundPod(ctx context.Context, token, audience string) (*corev1.Pod, error) {
	if strings.TrimSpace(token) == "" {
		return nil, workloadidentity.ErrUnauthorized
	}
	review, err := b.client.AuthenticationV1().TokenReviews().Create(ctx, &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{Token: token, Audiences: []string{audience}},
	}, metav1.CreateOptions{})
	if err != nil {
		return nil, workloadidentity.ErrUnavailable
	}
	matchedAudience := false
	for _, accepted := range review.Status.Audiences {
		matchedAudience = matchedAudience || accepted == audience
	}
	if !review.Status.Authenticated || !matchedAudience {
		return nil, workloadidentity.ErrUnauthorized
	}
	namespace, serviceAccount, ok := parseServiceAccountSubject(review.Status.User.Username)
	podName, podUID := userExtra(review.Status.User.Extra, podNameExtra), userExtra(review.Status.User.Extra, podUIDExtra)
	if !ok || podName == "" || podUID == "" {
		return nil, workloadidentity.ErrUnauthorized
	}
	pod, err := b.client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, workloadidentity.ErrUnauthorized
	}
	if err != nil {
		return nil, workloadidentity.ErrUnavailable
	}
	if string(pod.UID) != podUID || pod.Spec.ServiceAccountName != serviceAccount {
		return nil, workloadidentity.ErrUnauthorized
	}
	return pod, nil
}

func (b *Backend) AuthenticateWorkload(ctx context.Context, token string) (ports.RuntimeWorkloadIdentity, error) {
	pod, err := b.authenticateBoundPod(ctx, token, b.config.ServiceAccountAudience)
	if err != nil {
		return ports.RuntimeWorkloadIdentity{}, err
	}
	identity := ports.RuntimeWorkloadIdentity{
		Namespace: pod.Namespace, ServiceAccount: pod.Spec.ServiceAccountName, PodName: pod.Name, PodUID: string(pod.UID),
		RuntimeID: pod.Labels[labelRuntimeID],
	}
	switch identity.ServiceAccount {
	case commandServiceAccount:
		return ports.RuntimeWorkloadIdentity{}, workloadidentity.ErrUnauthorized
	case runtimeWorkerServiceAccount:
		if pod.Labels[labelManagedBy] != "druid" || pod.Labels[labelComponent] != "worker-pull" || identity.RuntimeID == "" {
			return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("invalid Druid worker workload")
		}
		identity.Kind = "worker"
	default:
		if !b.isOperatorServiceAccount(pod.Namespace, identity.ServiceAccount) {
			return ports.RuntimeWorkloadIdentity{}, fmt.Errorf("service account is not permitted")
		}
		identity.Kind = "operator"
	}
	return identity, nil
}

func (b *Backend) ensureRuntimeServiceAccount(ctx context.Context, namespace string, name string) error {
	_, err := b.client.CoreV1().ServiceAccounts(namespace).Create(ctx, &corev1.ServiceAccount{
		ObjectMeta:                   metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{labelManagedBy: "druid", labelComponent: "workload-identity"}},
		AutomountServiceAccountToken: ptrBool(false),
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (b *Backend) ensureRuntimeServiceAccounts(ctx context.Context, namespace string) error {
	if err := b.ensureRuntimeServiceAccount(ctx, namespace, runtimeWorkerServiceAccount); err != nil {
		return err
	}
	return b.ensureRuntimeServiceAccount(ctx, namespace, commandServiceAccount)
}

func (b *Backend) isOperatorServiceAccount(namespace string, serviceAccount string) bool {
	expected := strings.TrimSpace(b.config.OperatorServiceAccount)
	return expected != "" && expected == namespace+"/"+serviceAccount
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
