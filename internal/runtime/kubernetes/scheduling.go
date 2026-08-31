package kubernetes

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// pinPodToRuntimeNode keeps concurrent users of an RWO runtime PVC on the
// node where its active runtime pod is already attached. Kubernetes permits
// multiple read-write mounts of an RWO volume on one node, but not across
// nodes.
func (b *Backend) pinPodToRuntimeNode(ctx context.Context, namespace string, pvc string, podSpec *corev1.PodSpec) error {
	nodeName, err := b.runtimePVCNode(ctx, namespace, pvc)
	if err != nil || nodeName == "" {
		return err
	}
	if podSpec.NodeSelector == nil {
		podSpec.NodeSelector = map[string]string{}
	}
	podSpec.NodeSelector[corev1.LabelHostname] = nodeName
	return nil
}

func (b *Backend) runtimePVCNode(ctx context.Context, namespace string, pvc string) (string, error) {
	pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{labelScrollID: dnsLabel(pvc)}).String(),
	})
	if err != nil {
		return "", err
	}
	var pendingNode string
	var pendingCreated time.Time
	for idx := range pods.Items {
		pod := &pods.Items[idx]
		if pod.Status.Phase != corev1.PodPending && pod.Status.Phase != corev1.PodRunning {
			continue
		}
		if pod.Spec.NodeName == "" || !podUsesPVC(pod, pvc) {
			continue
		}
		if pod.Status.Phase == corev1.PodRunning {
			return pod.Spec.NodeName, nil
		}
		created := pod.CreationTimestamp.Time
		if pendingNode == "" || created.Before(pendingCreated) {
			pendingNode = pod.Spec.NodeName
			pendingCreated = created
		}
	}
	return pendingNode, nil
}

func podUsesPVC(pod *corev1.Pod, pvc string) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvc {
			return true
		}
	}
	return false
}
