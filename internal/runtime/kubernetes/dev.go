package kubernetes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (b *Backend) StartDev(ctx context.Context, action ports.RuntimeDevAction) error {
	if b.config.PullImage == "" {
		logger.Log().Error("Cannot start Kubernetes dev server without pull image", zap.String("runtime_id", action.RuntimeID), zap.String("root_ref", action.RootRef))
		return fmt.Errorf("kubernetes dev requires --k8s-pull-image or DRUID_K8S_PULL_IMAGE")
	}
	if action.MountPath == "" {
		action.MountPath = "/scroll"
	}
	if action.Listen == "" {
		action.Listen = ":8084"
	}
	namespace, pvc, err := parseRef(action.RootRef)
	if err != nil {
		logger.Log().Error("Kubernetes dev root ref invalid", zap.String("runtime_id", action.RuntimeID), zap.String("root_ref", action.RootRef), zap.Error(err))
		return err
	}
	logger.Log().Info("Starting Kubernetes dev server",
		zap.String("runtime_id", action.RuntimeID),
		zap.String("namespace", namespace),
		zap.String("pvc", pvc),
		zap.String("listen", action.Listen),
	)
	logger.Log().Debug("Kubernetes dev server details",
		zap.String("runtime_id", action.RuntimeID),
		zap.String("root_ref", action.RootRef),
		zap.String("mount_path", action.MountPath),
		zap.Strings("watch_paths", action.WatchPaths),
		zap.Strings("commands", action.HotReloadCommands),
		zap.String("image", b.config.PullImage),
	)
	sts := devStatefulSetSpec(namespace, action.RootRef, pvc, b.config.PullImage, action, b.config.RegistrySecret)
	existing, err := b.client.AppsV1().StatefulSets(namespace).Get(ctx, sts.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		logger.Log().Info("Creating Kubernetes dev StatefulSet", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name))
		if _, err := b.client.AppsV1().StatefulSets(namespace).Create(ctx, sts, metav1.CreateOptions{}); err != nil {
			logger.Log().Error("Failed to create Kubernetes dev StatefulSet", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name), zap.Error(err))
			return err
		}
	case err != nil:
		logger.Log().Error("Failed to get Kubernetes dev StatefulSet", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name), zap.Error(err))
		return err
	default:
		logger.Log().Info("Updating Kubernetes dev StatefulSet", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name), zap.String("resource_version", existing.ResourceVersion))
		sts.ResourceVersion = existing.ResourceVersion
		if _, err := b.client.AppsV1().StatefulSets(namespace).Update(ctx, sts, metav1.UpdateOptions{}); err != nil {
			logger.Log().Error("Failed to update Kubernetes dev StatefulSet", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name), zap.Error(err))
			return err
		}
	}
	service := devServiceSpec(namespace, action.RootRef, pvc)
	if err := b.reconcileService(ctx, service); err != nil {
		logger.Log().Error("Failed to reconcile Kubernetes dev Service", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("service", service.Name), zap.Error(err))
		return err
	}
	if err := b.waitForStatefulSet(ctx, namespace, sts.Name); err != nil {
		logger.Log().Error("Kubernetes dev StatefulSet did not become ready", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name), zap.Error(err))
		return err
	}
	logger.Log().Info("Kubernetes dev server ready", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name), zap.String("service", service.Name))
	return nil
}

func (b *Backend) StopDev(ctx context.Context, root string) error {
	namespace, _, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot stop Kubernetes dev server for invalid root", zap.String("root", root), zap.Error(err))
		return err
	}
	propagation := metav1.DeletePropagationBackground
	statefulSetName := devStatefulSetName(root)
	serviceName := serviceName(root, "dev", "webdav")
	logger.Log().Info("Stopping Kubernetes dev server", zap.String("namespace", namespace), zap.String("statefulset", statefulSetName), zap.String("service", serviceName))
	if err := b.client.AppsV1().StatefulSets(namespace).Delete(ctx, statefulSetName, metav1.DeleteOptions{PropagationPolicy: &propagation}); err != nil && !apierrors.IsNotFound(err) {
		logger.Log().Warn("Failed to delete Kubernetes dev StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", statefulSetName), zap.Error(err))
	}
	if err := b.client.CoreV1().Services(namespace).Delete(ctx, serviceName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		logger.Log().Warn("Failed to delete Kubernetes dev Service", zap.String("namespace", namespace), zap.String("service", serviceName), zap.Error(err))
	}
	return nil
}

func (b *Backend) reconcileService(ctx context.Context, service *corev1.Service) error {
	logger.Log().Debug("Reconciling Kubernetes Service", zap.String("namespace", service.Namespace), zap.String("service", service.Name), zap.Any("selector", service.Spec.Selector), zap.Int("ports", len(service.Spec.Ports)))
	existing, err := b.client.CoreV1().Services(service.Namespace).Get(ctx, service.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		logger.Log().Info("Creating Kubernetes Service", zap.String("namespace", service.Namespace), zap.String("service", service.Name))
		_, err := b.client.CoreV1().Services(service.Namespace).Create(ctx, service, metav1.CreateOptions{})
		if err != nil {
			logger.Log().Error("Failed to create Kubernetes Service", zap.String("namespace", service.Namespace), zap.String("service", service.Name), zap.Error(err))
		}
		return err
	case err != nil:
		logger.Log().Error("Failed to get Kubernetes Service", zap.String("namespace", service.Namespace), zap.String("service", service.Name), zap.Error(err))
		return err
	}
	logger.Log().Info("Updating Kubernetes Service", zap.String("namespace", service.Namespace), zap.String("service", service.Name), zap.String("resource_version", existing.ResourceVersion))
	service.ResourceVersion = existing.ResourceVersion
	service.Spec.ClusterIP = existing.Spec.ClusterIP
	service.Spec.ClusterIPs = existing.Spec.ClusterIPs
	service.Spec.IPFamilies = existing.Spec.IPFamilies
	service.Spec.IPFamilyPolicy = existing.Spec.IPFamilyPolicy
	_, err = b.client.CoreV1().Services(service.Namespace).Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		logger.Log().Error("Failed to update Kubernetes Service", zap.String("namespace", service.Namespace), zap.String("service", service.Name), zap.Error(err))
	}
	return err
}
