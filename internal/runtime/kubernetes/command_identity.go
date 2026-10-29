package kubernetes

import (
	"context"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (b *Backend) ActiveExecutions(ctx context.Context) (map[string]bool, error) {
	selector := metav1.ListOptions{LabelSelector: workloadidentity.ExecutionLabel}
	jobs, err := b.client.BatchV1().Jobs("").List(ctx, selector)
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	for _, job := range jobs.Items {
		if job.Status.Succeeded == 0 && !kubernetesJobFailed(&job) && job.DeletionTimestamp == nil {
			active[job.Labels[workloadidentity.ExecutionLabel]] = true
		}
	}
	sets, err := b.client.AppsV1().StatefulSets("").List(ctx, selector)
	if err != nil {
		return nil, err
	}
	for _, set := range sets.Items {
		if set.DeletionTimestamp == nil && (set.Spec.Replicas == nil || *set.Spec.Replicas > 0) {
			active[set.Labels[workloadidentity.ExecutionLabel]] = true
		}
	}
	return active, nil
}
