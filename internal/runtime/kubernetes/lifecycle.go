package kubernetes

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (b *Backend) StopRuntime(root string) error {
	logger.Log().Info("Stopping Kubernetes runtime", zap.String("root", root))
	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{PropagationPolicy: &propagation}
	if err := b.deleteRuntimeJobs(context.Background(), root, options); err != nil {
		logger.Log().Error("Failed to delete Kubernetes runtime jobs", zap.String("root", root), zap.Error(err))
		return err
	}
	if err := b.deleteRuntimeStatefulSets(context.Background(), root, options); err != nil {
		logger.Log().Error("Failed to delete Kubernetes runtime StatefulSets", zap.String("root", root), zap.Error(err))
		return err
	}
	if err := b.deleteRuntimePodsByScroll(context.Background(), root, options); err != nil {
		logger.Log().Error("Failed to delete Kubernetes runtime pods", zap.String("root", root), zap.Error(err))
		return err
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := b.waitRuntimePodsByScrollDeleted(waitCtx, root); err != nil {
		logger.Log().Error("Timed out waiting for Kubernetes runtime pods to stop", zap.String("root", root), zap.Error(err))
		return err
	}
	logger.Log().Info("Stopped Kubernetes runtime", zap.String("root", root))
	return nil
}

func (b *Backend) DeleteRuntime(root string, purgeData bool) error {
	logger.Log().Info("Deleting Kubernetes runtime", zap.String("root", root), zap.Bool("purge_data", purgeData))
	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{PropagationPolicy: &propagation}
	if err := b.StopRuntime(root); err != nil {
		return err
	}
	if err := b.deleteRuntimeServices(context.Background(), root, options); err != nil {
		logger.Log().Error("Failed to delete Kubernetes runtime Services", zap.String("root", root), zap.Error(err))
		return err
	}
	if purgeData {
		namespace, pvc, err := parseRef(root)
		if err != nil {
			logger.Log().Error("Cannot purge Kubernetes runtime data for invalid root", zap.String("root", root), zap.Error(err))
			return err
		}
		logger.Log().Info("Deleting Kubernetes runtime PVC", zap.String("namespace", namespace), zap.String("pvc", pvc))
		err = b.client.CoreV1().PersistentVolumeClaims(namespace).Delete(context.Background(), pvc, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Log().Error("Failed to delete Kubernetes runtime PVC", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Error(err))
			return err
		}
	}
	logger.Log().Info("Deleted Kubernetes runtime", zap.String("root", root), zap.Bool("purge_data", purgeData))
	return nil
}

func (b *Backend) deleteRuntimeWorkload(ctx context.Context, root string, target string, options metav1.DeleteOptions) error {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot delete Kubernetes runtime workload for invalid root", zap.String("root", root), zap.String("target", target), zap.Error(err))
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID:  dnsLabel(pvc),
		labelProcedure: dnsLabel(target),
	}).String()
	logger.Log().Info("Deleting Kubernetes runtime workload", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("target", target), zap.String("selector", selector))
	jobs, jobErr := b.client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if jobErr == nil {
		logger.Log().Debug("Deleting Kubernetes workload jobs", zap.String("namespace", namespace), zap.String("target", target), zap.Int("jobs", len(jobs.Items)))
		for _, job := range jobs.Items {
			if err := b.client.BatchV1().Jobs(namespace).Delete(ctx, job.Name, options); err != nil && !apierrors.IsNotFound(err) {
				logger.Log().Error("Failed to delete Kubernetes workload job", zap.String("namespace", namespace), zap.String("job", job.Name), zap.String("target", target), zap.Error(err))
				jobErr = err
				break
			}
			logger.Log().Debug("Deleted Kubernetes workload job", zap.String("namespace", namespace), zap.String("job", job.Name), zap.String("target", target))
		}
	} else {
		logger.Log().Error("Failed to list Kubernetes workload jobs", zap.String("namespace", namespace), zap.String("target", target), zap.Error(jobErr))
	}
	statefulSets, statefulSetErr := b.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if statefulSetErr == nil {
		logger.Log().Debug("Deleting Kubernetes workload StatefulSets", zap.String("namespace", namespace), zap.String("target", target), zap.Int("statefulsets", len(statefulSets.Items)))
		for _, statefulSet := range statefulSets.Items {
			if err := b.client.AppsV1().StatefulSets(namespace).Delete(ctx, statefulSet.Name, options); err != nil && !apierrors.IsNotFound(err) {
				logger.Log().Error("Failed to delete Kubernetes workload StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.String("target", target), zap.Error(err))
				statefulSetErr = err
				break
			}
			logger.Log().Debug("Deleted Kubernetes workload StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.String("target", target))
		}
	} else {
		logger.Log().Error("Failed to list Kubernetes workload StatefulSets", zap.String("namespace", namespace), zap.String("target", target), zap.Error(statefulSetErr))
	}
	podErr := b.deleteRuntimePods(ctx, root, target, options)
	if jobErr != nil {
		return jobErr
	}
	if statefulSetErr != nil {
		return statefulSetErr
	}
	return podErr
}

func (b *Backend) deleteRuntimePods(ctx context.Context, root string, target string, options metav1.DeleteOptions) error {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot delete Kubernetes runtime pods for invalid root", zap.String("root", root), zap.String("target", target), zap.Error(err))
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID:  dnsLabel(pvc),
		labelProcedure: dnsLabel(target),
	}).String()
	pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		logger.Log().Error("Failed to list Kubernetes runtime pods", zap.String("namespace", namespace), zap.String("target", target), zap.String("selector", selector), zap.Error(err))
		return err
	}
	logger.Log().Debug("Deleting Kubernetes runtime pods", zap.String("namespace", namespace), zap.String("target", target), zap.String("selector", selector), zap.Int("pods", len(pods.Items)))
	for _, pod := range pods.Items {
		if err := b.client.CoreV1().Pods(namespace).Delete(ctx, pod.Name, options); err != nil && !apierrors.IsNotFound(err) {
			logger.Log().Error("Failed to delete Kubernetes runtime pod", zap.String("namespace", namespace), zap.String("pod", pod.Name), zap.String("target", target), zap.Error(err))
			return err
		}
		logger.Log().Debug("Deleted Kubernetes runtime pod", zap.String("namespace", namespace), zap.String("pod", pod.Name), zap.String("target", target))
	}
	return nil
}

func (b *Backend) deleteRuntimeJobs(ctx context.Context, root string, options metav1.DeleteOptions) error {
	namespace, _, err := parseRef(root)
	if err != nil {
		return err
	}
	return b.deleteRuntimeObjects(ctx, root, func(name string) error {
		err := b.client.BatchV1().Jobs(namespace).Delete(ctx, name, options)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}, "jobs")
}

func (b *Backend) deleteRuntimeStatefulSets(ctx context.Context, root string, options metav1.DeleteOptions) error {
	namespace, _, err := parseRef(root)
	if err != nil {
		return err
	}
	return b.deleteRuntimeObjects(ctx, root, func(name string) error {
		err := b.client.AppsV1().StatefulSets(namespace).Delete(ctx, name, options)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}, "statefulsets")
}

func (b *Backend) deleteRuntimeServices(ctx context.Context, root string, options metav1.DeleteOptions) error {
	namespace, _, err := parseRef(root)
	if err != nil {
		return err
	}
	return b.deleteRuntimeObjects(ctx, root, func(name string) error {
		err := b.client.CoreV1().Services(namespace).Delete(ctx, name, options)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}, "services")
}

func (b *Backend) deleteRuntimeObjects(ctx context.Context, root string, deleteOne func(name string) error, kind string) error {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot delete Kubernetes runtime objects for invalid root", zap.String("root", root), zap.String("kind", kind), zap.Error(err))
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID: dnsLabel(pvc),
	}).String()
	logger.Log().Debug("Deleting Kubernetes runtime objects", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("kind", kind), zap.String("selector", selector))
	switch kind {
	case "jobs":
		items, err := b.client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			logger.Log().Error("Failed to list Kubernetes runtime jobs", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Error(err))
			return err
		}
		logger.Log().Debug("Listed Kubernetes runtime jobs", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Int("jobs", len(items.Items)))
		for _, item := range items.Items {
			if err := deleteOne(item.Name); err != nil {
				logger.Log().Error("Failed to delete Kubernetes runtime job", zap.String("namespace", namespace), zap.String("job", item.Name), zap.Error(err))
				return err
			}
			logger.Log().Debug("Deleted Kubernetes runtime job", zap.String("namespace", namespace), zap.String("job", item.Name))
		}
	case "statefulsets":
		items, err := b.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			logger.Log().Error("Failed to list Kubernetes runtime StatefulSets", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Error(err))
			return err
		}
		logger.Log().Debug("Listed Kubernetes runtime StatefulSets", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Int("statefulsets", len(items.Items)))
		for _, item := range items.Items {
			if err := deleteOne(item.Name); err != nil {
				logger.Log().Error("Failed to delete Kubernetes runtime StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", item.Name), zap.Error(err))
				return err
			}
			logger.Log().Debug("Deleted Kubernetes runtime StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", item.Name))
		}
	case "services":
		items, err := b.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			logger.Log().Error("Failed to list Kubernetes runtime Services", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Error(err))
			return err
		}
		logger.Log().Debug("Listed Kubernetes runtime Services", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Int("services", len(items.Items)))
		for _, item := range items.Items {
			if err := deleteOne(item.Name); err != nil {
				logger.Log().Error("Failed to delete Kubernetes runtime Service", zap.String("namespace", namespace), zap.String("service", item.Name), zap.Error(err))
				return err
			}
			logger.Log().Debug("Deleted Kubernetes runtime Service", zap.String("namespace", namespace), zap.String("service", item.Name))
		}
	}
	return nil
}

func (b *Backend) deleteRuntimePodsByScroll(ctx context.Context, root string, options metav1.DeleteOptions) error {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID: dnsLabel(pvc),
	}).String()
	pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		logger.Log().Error("Failed to list Kubernetes runtime pods by scroll", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("selector", selector), zap.Error(err))
		return err
	}
	logger.Log().Debug("Deleting Kubernetes runtime pods by scroll", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("selector", selector), zap.Int("pods", len(pods.Items)))
	for _, pod := range pods.Items {
		if err := b.client.CoreV1().Pods(namespace).Delete(ctx, pod.Name, options); err != nil && !apierrors.IsNotFound(err) {
			logger.Log().Error("Failed to delete Kubernetes runtime pod by scroll", zap.String("namespace", namespace), zap.String("pod", pod.Name), zap.Error(err))
			return err
		}
		logger.Log().Debug("Deleted Kubernetes runtime pod by scroll", zap.String("namespace", namespace), zap.String("pod", pod.Name))
	}
	return nil
}

func (b *Backend) waitRuntimePodsByScrollDeleted(ctx context.Context, root string) error {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID: dnsLabel(pvc),
	}).String()
	backoff := newCappedBackoff(podPollInitial, podPollMax)
	for {
		pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}
		if len(pods.Items) == 0 {
			return nil
		}
		names := make([]string, 0, len(pods.Items))
		for _, pod := range pods.Items {
			names = append(names, pod.Name)
		}
		logger.Log().Debug("Waiting for Kubernetes runtime pods to stop", zap.String("namespace", namespace), zap.String("selector", selector), zap.Strings("pods", names))
		if err := sleepWithContext(ctx, backoff.Next()); err != nil {
			return fmt.Errorf("pods still present for %s: %v: %w", selector, names, err)
		}
	}
}
