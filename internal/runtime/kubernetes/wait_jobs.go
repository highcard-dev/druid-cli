package kubernetes

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (b *Backend) waitForJobPod(ctx context.Context, namespace string, jobName string, controllerUID string) (string, error) {
	matchLabels := labels.Set{"job-name": jobName}
	if controllerUID != "" {
		matchLabels["controller-uid"] = controllerUID
	}
	selector := labels.SelectorFromSet(matchLabels).String()
	logger.Log().Debug("Waiting for Kubernetes job pod", zap.String("namespace", namespace), zap.String("job", jobName), zap.String("selector", selector), zap.String("controller_uid", controllerUID))
	return b.waitForPodBySelector(ctx, namespace, selector)
}

func (b *Backend) waitForPodBySelector(ctx context.Context, namespace string, selector string) (string, error) {
	deadline := time.Now().Add(2 * time.Minute)
	backoff := newCappedBackoff(podPollInitial, podPollMax)
	for {
		pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			logger.Log().Error("Failed to list Kubernetes pods while waiting", zap.String("namespace", namespace), zap.String("selector", selector), zap.Error(err))
			return "", err
		}
		if len(pods.Items) > 0 {
			logger.Log().Debug("Kubernetes pod matched selector", zap.String("namespace", namespace), zap.String("selector", selector), zap.String("pod", pods.Items[0].Name), zap.Int("matches", len(pods.Items)))
			return pods.Items[0].Name, nil
		}
		if time.Now().After(deadline) {
			logger.Log().Error("Timed out waiting for Kubernetes pod", zap.String("namespace", namespace), zap.String("selector", selector))
			return "", fmt.Errorf("timed out waiting for pod matching selector %s", selector)
		}
		sleep := backoff.Next()
		logger.Log().Debug("No Kubernetes pod matched yet", zap.String("namespace", namespace), zap.String("selector", selector), zap.Duration("sleep", sleep), zap.Time("deadline", deadline))
		if err := sleepUntilNextPoll(ctx, deadline, sleep); err != nil {
			logger.Log().Warn("Stopped waiting for Kubernetes pod", zap.String("namespace", namespace), zap.String("selector", selector), zap.Error(err))
			return "", err
		}
	}
}

func (b *Backend) waitForStatefulSet(ctx context.Context, namespace string, name string) error {
	deadline := time.Now().Add(5 * time.Minute)
	backoff := newCappedBackoff(statefulSetPollInitial, statefulSetPollMax)
	for {
		statefulSet, err := b.client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			logger.Log().Error("Failed to get Kubernetes StatefulSet while waiting", zap.String("namespace", namespace), zap.String("statefulset", name), zap.Error(err))
			return err
		}
		wanted := int32(1)
		if statefulSet.Spec.Replicas != nil {
			wanted = *statefulSet.Spec.Replicas
		}
		if statefulSet.Status.ReadyReplicas >= wanted {
			logger.Log().Debug("Kubernetes StatefulSet ready", zap.String("namespace", namespace), zap.String("statefulset", name), zap.Int32("ready", statefulSet.Status.ReadyReplicas), zap.Int32("wanted", wanted))
			return nil
		}
		if time.Now().After(deadline) {
			logger.Log().Error("Timed out waiting for Kubernetes StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", name), zap.Int32("ready", statefulSet.Status.ReadyReplicas), zap.Int32("wanted", wanted))
			return fmt.Errorf("timed out waiting for StatefulSet %s to become ready", name)
		}
		sleep := backoff.Next()
		logger.Log().Debug("Kubernetes StatefulSet not ready yet", zap.String("namespace", namespace), zap.String("statefulset", name), zap.Int32("ready", statefulSet.Status.ReadyReplicas), zap.Int32("wanted", wanted), zap.Duration("sleep", sleep), zap.Time("deadline", deadline))
		if err := sleepUntilNextPoll(ctx, deadline, sleep); err != nil {
			logger.Log().Warn("Stopped waiting for Kubernetes StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", name), zap.Error(err))
			return err
		}
	}
}

func (b *Backend) waitForJob(ctx context.Context, namespace string, jobName string) (*int, error) {
	startedAt := time.Now()
	deadline := time.Now().Add(24 * time.Hour)
	for {
		job, err := b.client.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				if exitCode, ok := b.recentJobExit(namespace, jobName); ok {
					logger.Log().Info("Kubernetes job was already completed by another waiter", zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
					return exitCode, nil
				}
			}
			logger.Log().Error("Failed to get Kubernetes job while waiting", zap.String("namespace", namespace), zap.String("job", jobName), zap.Error(err))
			return nil, err
		}
		if job.Status.Succeeded > 0 {
			exitCode := 0
			b.recordJobExit(namespace, jobName, exitCode)
			logger.Log().Debug("Kubernetes job succeeded", zap.String("namespace", namespace), zap.String("job", jobName), zap.Int32("succeeded", job.Status.Succeeded), zap.Int32("failed", job.Status.Failed), zap.Int32("active", job.Status.Active))
			return &exitCode, nil
		}
		if kubernetesJobFailed(job) {
			exitCode, detail := b.jobFailure(ctx, namespace, jobName)
			logger.Log().Error("Kubernetes job failed", zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", exitCode), zap.String("detail", detail), zap.Int32("succeeded", job.Status.Succeeded), zap.Int32("failed", job.Status.Failed), zap.Int32("active", job.Status.Active))
			return &exitCode, fmt.Errorf("job %s failed: %s", jobName, detail)
		}
		if time.Since(startedAt) > time.Minute {
			if exitCode, detail, ok := b.jobStartupFailure(ctx, namespace, jobName); ok {
				logger.Log().Error("Kubernetes job startup failed", zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", exitCode), zap.String("detail", detail), zap.Int32("succeeded", job.Status.Succeeded), zap.Int32("failed", job.Status.Failed), zap.Int32("active", job.Status.Active))
				return &exitCode, fmt.Errorf("job %s failed: %s", jobName, detail)
			}
		}
		if time.Now().After(deadline) {
			logger.Log().Error("Timed out waiting for Kubernetes job", zap.String("namespace", namespace), zap.String("job", jobName), zap.Int32("succeeded", job.Status.Succeeded), zap.Int32("failed", job.Status.Failed), zap.Int32("active", job.Status.Active))
			return nil, fmt.Errorf("timed out waiting for job %s", jobName)
		}
		sleep := jobPollInterval(time.Since(startedAt))
		logger.Log().Debug("Kubernetes job still running", zap.String("namespace", namespace), zap.String("job", jobName), zap.Int32("succeeded", job.Status.Succeeded), zap.Int32("failed", job.Status.Failed), zap.Int32("active", job.Status.Active), zap.Duration("sleep", sleep), zap.Time("deadline", deadline))
		if err := sleepUntilNextPoll(ctx, deadline, sleep); err != nil {
			logger.Log().Warn("Stopped waiting for Kubernetes job", zap.String("namespace", namespace), zap.String("job", jobName), zap.Error(err))
			return nil, err
		}
	}
}

func (b *Backend) jobStartupFailure(ctx context.Context, namespace string, jobName string) (int, string, bool) {
	selector := labels.SelectorFromSet(labels.Set{"job-name": jobName}).String()
	pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil || len(pods.Items) == 0 {
		return 1, "", false
	}
	for _, pod := range pods.Items {
		for _, status := range pod.Status.InitContainerStatuses {
			if exitCode, detail, ok := startupContainerFailure(pod.Name, status); ok {
				return exitCode, detail, true
			}
		}
		for _, status := range pod.Status.ContainerStatuses {
			if exitCode, detail, ok := startupContainerFailure(pod.Name, status); ok {
				return exitCode, detail, true
			}
		}
	}
	return 1, "", false
}

func kubernetesJobFailed(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (b *Backend) jobFailure(ctx context.Context, namespace string, jobName string) (int, string) {
	selector := labels.SelectorFromSet(labels.Set{"job-name": jobName}).String()
	pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil || len(pods.Items) == 0 {
		if err != nil {
			return 1, err.Error()
		}
		return 1, "no pod was found for failed job"
	}
	pod := pods.Items[0]
	for _, status := range pod.Status.InitContainerStatuses {
		if exitCode, detail, ok := containerFailure(pod.Name, status); ok {
			return exitCode, detail
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if exitCode, detail, ok := containerFailure(pod.Name, status); ok {
			return exitCode, detail
		}
	}
	if pod.Status.Reason != "" || pod.Status.Message != "" {
		return 1, strings.TrimSpace(fmt.Sprintf("pod %s %s %s", pod.Name, pod.Status.Reason, pod.Status.Message))
	}
	return 1, fmt.Sprintf("pod %s phase %s", pod.Name, pod.Status.Phase)
}

func containerFailure(podName string, status corev1.ContainerStatus) (int, string, bool) {
	if status.State.Terminated != nil {
		terminated := status.State.Terminated
		detail := strings.TrimSpace(fmt.Sprintf("pod %s container %s terminated: reason=%s exit_code=%d %s", podName, status.Name, terminated.Reason, terminated.ExitCode, terminated.Message))
		return int(terminated.ExitCode), detail, true
	}
	if status.State.Waiting != nil {
		waiting := status.State.Waiting
		detail := strings.TrimSpace(fmt.Sprintf("pod %s container %s waiting: reason=%s %s", podName, status.Name, waiting.Reason, waiting.Message))
		return 1, detail, true
	}
	return 1, "", false
}

func startupContainerFailure(podName string, status corev1.ContainerStatus) (int, string, bool) {
	if terminated := status.State.Terminated; terminated != nil {
		if terminated.ExitCode == 0 {
			return 0, "", false
		}
		return containerFailure(podName, status)
	}
	if status.State.Waiting == nil {
		return 1, "", false
	}
	waiting := status.State.Waiting
	switch waiting.Reason {
	case "ErrImagePull", "ImagePullBackOff", "InvalidImageName", "CreateContainerConfigError", "CreateContainerError", "RunContainerError", "CrashLoopBackOff":
		return containerFailure(podName, status)
	default:
		return 1, "", false
	}
}

func (b *Backend) podLogs(ctx context.Context, namespace string, podName string) ([]byte, error) {
	logger.Log().Debug("Reading Kubernetes pod logs", zap.String("namespace", namespace), zap.String("pod", podName))
	req := b.client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{})
	stream, err := req.Stream(ctx)
	if err != nil {
		logger.Log().Warn("Failed to open Kubernetes pod log stream", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
		return nil, err
	}
	defer stream.Close()
	logs, err := io.ReadAll(stream)
	if err != nil {
		logger.Log().Warn("Failed to read Kubernetes pod logs", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
		return logs, err
	}
	logger.Log().Debug("Read Kubernetes pod logs", zap.String("namespace", namespace), zap.String("pod", podName), zap.Int("bytes", len(logs)))
	return logs, nil
}

func (b *Backend) streamPodLogs(ctx context.Context, namespace string, podName string, output chan<- string) {
	defer close(output)
	var stream io.ReadCloser
	deadline := time.Now().Add(30 * time.Second)
	logger.Log().Debug("Opening Kubernetes follow log stream", zap.String("namespace", namespace), zap.String("pod", podName))
	for {
		req := b.client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{Follow: true})
		var err error
		stream, err = req.Stream(ctx)
		if err == nil {
			logger.Log().Debug("Kubernetes follow log stream opened", zap.String("namespace", namespace), zap.String("pod", podName))
			break
		}
		if !strings.Contains(err.Error(), "ContainerCreating") &&
			!strings.Contains(err.Error(), "PodInitializing") &&
			!strings.Contains(err.Error(), "not available") {
			logger.Log().Warn("Failed to stream Kubernetes pod logs", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
			output <- fmt.Sprintf("failed to stream pod logs: %v", err)
			return
		}
		if time.Now().After(deadline) {
			logger.Log().Warn("Timed out opening Kubernetes pod log stream", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
			output <- fmt.Sprintf("failed to stream pod logs: %v", err)
			return
		}
		logger.Log().Debug("Kubernetes pod logs not ready yet", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
		select {
		case <-ctx.Done():
			logger.Log().Warn("Context cancelled while opening Kubernetes pod logs", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(ctx.Err()))
			output <- fmt.Sprintf("failed to stream pod logs: %v", ctx.Err())
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	defer stream.Close()
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		output <- scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		logger.Log().Warn("Kubernetes pod log stream ended with scanner error", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
		return
	}
	logger.Log().Debug("Kubernetes pod log stream ended", zap.String("namespace", namespace), zap.String("pod", podName))
}
