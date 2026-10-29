package kubernetes

import (
	"context"
	"fmt"
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func (b *Backend) ReadConsoleLogs(ctx context.Context, root, procedure string, tail int, maxBytes int64) (string, bool, error) {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return "", false, err
	}
	pod, name, _, err := b.waitForConsoleContainer(ctx, namespace, labels.SelectorFromSet(baseLabels(pvc)).String(), procedure)
	if err != nil {
		return "", false, err
	}
	tailLines, limit := int64(tail), maxBytes+1
	stream, err := b.client.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: name, TailLines: &tailLines, LimitBytes: &limit, Follow: false}).Stream(ctx)
	if err != nil {
		return "", false, err
	}
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, limit))
	if err != nil {
		return "", false, err
	}
	truncated := int64(len(data)) > maxBytes
	if truncated {
		data = data[:maxBytes]
	}
	return string(data), truncated, nil
}

func (b *Backend) WriteConsoleInput(ctx context.Context, root, procedure string, data []byte) (int, error) {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return 0, err
	}
	pod, name, running, err := b.waitForConsoleContainer(ctx, namespace, labels.SelectorFromSet(baseLabels(pvc)).String(), procedure)
	if err != nil {
		return 0, err
	}
	if !running {
		return 0, fmt.Errorf("console is not running")
	}
	if err := b.attachToPod(ctx, namespace, pod.Name, name, string(data)); err != nil {
		return 0, err
	}
	return len(data), nil
}
