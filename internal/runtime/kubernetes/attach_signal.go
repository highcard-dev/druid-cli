package kubernetes

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type consoleSession struct {
	io.ReadCloser
	write func([]byte) (int, error)
}

func (s *consoleSession) Write(data []byte) (int, error) {
	if s.write == nil {
		return 0, fmt.Errorf("console input is unavailable")
	}
	return s.write(data)
}

func (b *Backend) OpenConsole(ctx context.Context, root string, procedure string) (io.ReadWriteCloser, error) {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return nil, err
	}
	selector := baseLabels(pvc)
	labelSelector := labels.SelectorFromSet(selector).String()
	pod, containerName, running, err := b.waitForConsoleContainer(ctx, namespace, labelSelector, procedure)
	if err != nil {
		return nil, err
	}
	if pod == nil || containerName == "" {
		return nil, fmt.Errorf("no pod found for procedure %s", procedure)
	}
	stream, err := b.client.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container: containerName,
		Follow:    running,
	}).Stream(ctx)
	if err != nil {
		return nil, err
	}
	session := &consoleSession{ReadCloser: stream}
	if running {
		session.write = func(data []byte) (int, error) {
			if err := b.attachToPod(ctx, namespace, pod.Name, containerName, string(data)); err != nil {
				return 0, err
			}
			return len(data), nil
		}
	}
	return session, nil
}

func (b *Backend) waitForConsoleContainer(ctx context.Context, namespace string, selector string, procedure string) (*corev1.Pod, string, bool, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return nil, "", false, err
		}
		pod, containerName, running := selectProcedureConsole(pods.Items, procedure)
		if pod != nil {
			return pod, containerName, running, nil
		}
		if time.Now().After(deadline) {
			return nil, "", false, fmt.Errorf("no pod found for procedure %s", procedure)
		}
		select {
		case <-ctx.Done():
			return nil, "", false, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func selectProcedureConsole(pods []corev1.Pod, procedure string) (*corev1.Pod, string, bool) {
	type candidate struct {
		pod       *corev1.Pod
		container string
		running   bool
	}
	candidates := []candidate{}
	for idx := range pods {
		pod := &pods[idx]
		if pod.Labels[labelProcedure] == dnsLabel(procedure) {
			candidates = append(candidates, candidate{pod: pod, container: "main", running: containerIsRunning(pod.Status.ContainerStatuses, "main")})
			continue
		}
		initName := persistentInitContainerName(procedure)
		for _, container := range pod.Spec.InitContainers {
			if container.Name == initName {
				candidates = append(candidates, candidate{pod: pod, container: initName, running: containerIsRunning(pod.Status.InitContainerStatuses, initName)})
				break
			}
		}
	}
	if len(candidates) == 0 {
		return nil, "", false
	}
	selected := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.running && !selected.running || candidate.running == selected.running && candidate.pod.CreationTimestamp.After(selected.pod.CreationTimestamp.Time) {
			selected = candidate
		}
	}
	return selected.pod, selected.container, selected.running
}

func containerIsRunning(statuses []corev1.ContainerStatus, name string) bool {
	for _, status := range statuses {
		if status.Name == name {
			return status.State.Running != nil
		}
	}
	return false
}

func selectConsolePod(pods []corev1.Pod) *corev1.Pod {
	if len(pods) == 0 {
		return nil
	}
	selected := &pods[0]
	for index := 1; index < len(pods); index++ {
		candidate := &pods[index]
		candidateRunning := candidate.Status.Phase == corev1.PodRunning
		selectedRunning := selected.Status.Phase == corev1.PodRunning
		if candidateRunning && !selectedRunning || candidateRunning == selectedRunning && candidate.CreationTimestamp.After(selected.CreationTimestamp.Time) {
			selected = candidate
		}
	}
	return selected
}

func (b *Backend) attachToPod(ctx context.Context, namespace string, podName string, containerName string, data string) error {
	logger.Log().Debug("Opening Kubernetes pod attach stream", zap.String("namespace", namespace), zap.String("pod", podName), zap.Int("bytes", len(data)))
	req := b.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("attach").
		VersionedParams(&corev1.PodAttachOptions{
			Container: containerName,
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
