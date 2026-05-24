package kubernetes

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (b *Backend) Attach(commandName string, data string) error {
	logger.Log().Debug("Attaching to Kubernetes procedure by command name", zap.String("command", commandName), zap.Int("bytes", len(data)))
	pods, err := b.client.CoreV1().Pods(b.config.Namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{labelProcedure: dnsLabel(commandName)}).String(),
	})
	if err != nil {
		logger.Log().Error("Failed to list Kubernetes pods for attach", zap.String("namespace", b.config.Namespace), zap.String("command", commandName), zap.Error(err))
		return err
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			logger.Log().Debug("Attaching to Kubernetes pod", zap.String("namespace", b.config.Namespace), zap.String("pod", pod.Name), zap.String("command", commandName), zap.Int("bytes", len(data)))
			return b.attachToPod(context.Background(), b.config.Namespace, pod.Name, data)
		}
	}
	logger.Log().Warn("No running Kubernetes pod found for attach", zap.String("namespace", b.config.Namespace), zap.String("command", commandName), zap.Int("pods", len(pods.Items)))
	return fmt.Errorf("no running pod found for console %s", commandName)
}

func (b *Backend) attachToProcedure(root string, procedureName string, data string) error {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot attach to Kubernetes procedure for invalid root", zap.String("root", root), zap.String("procedure", procedureName), zap.Error(err))
		return err
	}
	selector := baseLabels(pvc)
	selector[labelProcedure] = dnsLabel(procedureName)
	podName, err := b.waitForPodBySelector(context.Background(), namespace, labels.SelectorFromSet(selector).String())
	if err != nil {
		logger.Log().Error("Failed to find Kubernetes procedure pod for attach", zap.String("namespace", namespace), zap.String("procedure", procedureName), zap.Any("selector", selector), zap.Error(err))
		return err
	}
	logger.Log().Debug("Attaching to Kubernetes procedure pod", zap.String("namespace", namespace), zap.String("pod", podName), zap.String("procedure", procedureName), zap.Int("bytes", len(data)))
	return b.attachToPod(context.Background(), namespace, podName, data)
}

func (b *Backend) attachToPod(ctx context.Context, namespace string, podName string, data string) error {
	logger.Log().Debug("Opening Kubernetes pod attach stream", zap.String("namespace", namespace), zap.String("pod", podName), zap.Int("bytes", len(data)))
	req := b.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("attach").
		VersionedParams(&corev1.PodAttachOptions{
			Container: "main",
			Stdin:     true,
			Stdout:    false,
			Stderr:    false,
			TTY:       false,
		}, k8sscheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(b.restConfig, "POST", req.URL())
	if err != nil {
		logger.Log().Error("Failed to create Kubernetes pod attach executor", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
		return err
	}
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin: strings.NewReader(data),
	}); err != nil {
		logger.Log().Error("Kubernetes pod attach stream failed", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
		return err
	}
	logger.Log().Debug("Kubernetes pod attach stream completed", zap.String("namespace", namespace), zap.String("pod", podName))
	return nil
}

func (b *Backend) Signal(_ string, target string, signal string, root string) error {
	if target == "" {
		logger.Log().Warn("Ignoring Kubernetes signal with empty target", zap.String("root", root), zap.String("signal", signal))
		return nil
	}
	logger.Log().Info("Sending Kubernetes runtime signal", zap.String("root", root), zap.String("target", target), zap.String("signal", signal))
	switch signal {
	case "", "SIGTERM", "TERM":
		propagation := metav1.DeletePropagationBackground
		return b.deleteRuntimeWorkload(context.Background(), root, target, metav1.DeleteOptions{PropagationPolicy: &propagation})
	case "SIGKILL", "KILL":
		grace := int64(0)
		propagation := metav1.DeletePropagationBackground
		return b.deleteRuntimeWorkload(context.Background(), root, target, metav1.DeleteOptions{GracePeriodSeconds: &grace, PropagationPolicy: &propagation})
	default:
		logger.Log().Error("Unsupported Kubernetes signal", zap.String("root", root), zap.String("target", target), zap.String("signal", signal))
		return fmt.Errorf("kubernetes signal %s is unsupported without pod exec", signal)
	}
}

func runtimeConsoleID(scrollID string, procedureName string) string {
	if scrollID == "" {
		return procedureName
	}
	return scrollID + "/" + procedureName
}
