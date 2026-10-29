package kubernetes

import (
	"context"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	corev1 "k8s.io/api/core/v1"
)

const commandServiceAccount = "druid-workload-agent"
const commandTokenAudience = "druid-command-tools"
const commandDeploymentAnnotation = "druid.gg/deployment-id"

func commandIdentityAnnotations(identity *domain.RuntimeCommandIdentity, command string) map[string]string {
	if identity == nil || !identity.Enabled {
		return nil
	}
	return map[string]string{commandDeploymentAnnotation: identity.DeploymentID, labelCommand: command}
}

func (b *Backend) PrepareCommandIdentity(ctx context.Context, record workloadidentity.Record) (workloadidentity.CredentialMount, error) {
	namespace, _, err := parseRef(record.RuntimeRoot)
	if err == nil {
		err = b.ensureRuntimeServiceAccount(ctx, namespace, commandServiceAccount)
	}
	return workloadidentity.CredentialMount{}, err
}

func (b *Backend) AuthenticateCommandIdentity(ctx context.Context, token string) (workloadidentity.Record, error) {
	pod, err := b.authenticateBoundPod(ctx, token, commandTokenAudience)
	if err != nil {
		return workloadidentity.Record{}, err
	}
	deployment, command := pod.Annotations[commandDeploymentAnnotation], pod.Annotations[labelCommand]
	execution := pod.Labels[workloadidentity.ExecutionLabel]
	if pod.Spec.ServiceAccountName != commandServiceAccount || pod.DeletionTimestamp != nil || (pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending) || pod.Labels[labelManagedBy] != "druid" || pod.Labels[labelComponent] != "runtime" || deployment == "" || command == "" || execution == "" || pod.Labels[labelCommand] != dnsLabel(command) {
		return workloadidentity.Record{}, workloadidentity.ErrUnauthorized
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == "data" && volume.PersistentVolumeClaim != nil && pod.Labels[labelScrollID] == dnsLabel(volume.PersistentVolumeClaim.ClaimName) {
			root := ref(pod.Namespace, volume.PersistentVolumeClaim.ClaimName)
			if pod.Labels[labelRuntimeID] != dnsLabel(runtimeID(root)) {
				return workloadidentity.Record{}, workloadidentity.ErrUnauthorized
			}
			return workloadidentity.Record{RuntimeRoot: root, DeploymentID: deployment, Command: command, ExecutionID: execution}, nil
		}
	}
	return workloadidentity.Record{}, workloadidentity.ErrUnauthorized
}
