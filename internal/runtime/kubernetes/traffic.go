package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type nodeStatsReader func(context.Context, string) (*nodeStatsSummary, error)

type nodeStatsSummary struct {
	Pods []nodePodStats `json:"pods"`
}

type nodePodStats struct {
	PodRef struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		UID       string `json:"uid"`
	} `json:"podRef"`
	Network *nodeNetworkStats `json:"network,omitempty"`
}

type nodeNetworkStats struct {
	RXBytes    *uint64                `json:"rxBytes,omitempty"`
	TXBytes    *uint64                `json:"txBytes,omitempty"`
	Interfaces []nodeNetworkInterface `json:"interfaces,omitempty"`
}

type nodeNetworkInterface struct {
	RXBytes *uint64 `json:"rxBytes,omitempty"`
	TXBytes *uint64 `json:"txBytes,omitempty"`
}

type podTraffic struct {
	podUID         string
	rxBytes        uint64
	txBytes        uint64
	lastDeltaRX    uint64
	lastActivityAt *time.Time
	samples        []podTrafficSample
}

type podTrafficSample struct {
	at time.Time
	rx uint64
	tx uint64
}

type podTrafficStore struct {
	mu             sync.Mutex
	samples        map[string][]podTrafficSample
	lastActivityAt map[string]time.Time
}

var globalPodTrafficStore = &podTrafficStore{
	samples:        map[string][]podTrafficSample{},
	lastActivityAt: map[string]time.Time{},
}

func (s *podTrafficStore) record(podUID string, rxBytes uint64, txBytes uint64, now time.Time) podTraffic {
	s.mu.Lock()
	defer s.mu.Unlock()

	samples := s.samples[podUID]
	var lastDeltaRX uint64
	if len(samples) > 0 && rxBytes >= samples[len(samples)-1].rx {
		lastDeltaRX = rxBytes - samples[len(samples)-1].rx
		if lastDeltaRX > 0 {
			s.lastActivityAt[podUID] = now
		}
	}
	samples = append(samples, podTrafficSample{at: now, rx: rxBytes, tx: txBytes})
	cutoff := now.Add(-24 * time.Hour)
	keepFrom := 0
	for keepFrom < len(samples) && samples[keepFrom].at.Before(cutoff) {
		keepFrom++
	}
	samples = samples[keepFrom:]
	s.samples[podUID] = samples

	var lastActivityAt *time.Time
	if last, ok := s.lastActivityAt[podUID]; ok {
		lastCopy := last
		lastActivityAt = &lastCopy
	}

	return podTraffic{
		podUID:         podUID,
		rxBytes:        rxBytes,
		txBytes:        txBytes,
		lastDeltaRX:    lastDeltaRX,
		lastActivityAt: lastActivityAt,
		samples:        append([]podTrafficSample(nil), samples...),
	}
}

func (t podTraffic) rxDelta(window time.Duration, now time.Time) uint64 {
	if window <= 0 || len(t.samples) == 0 {
		return t.lastDeltaRX
	}
	cutoff := now.Add(-window)
	base := t.samples[0]
	for _, sample := range t.samples {
		if !sample.at.Before(cutoff) {
			base = sample
			break
		}
	}
	if t.rxBytes < base.rx {
		return 0
	}
	return t.rxBytes - base.rx
}

func (t podTraffic) windowReady(window time.Duration, now time.Time) bool {
	if window <= 0 {
		return len(t.samples) > 1
	}
	if len(t.samples) < 2 {
		return false
	}
	return !t.samples[0].at.After(now.Add(-window))
}

type procedureTraffic struct {
	pods           []podTraffic
	rxBytes        uint64
	txBytes        uint64
	lastActivityAt *time.Time
}

func (t procedureTraffic) rxDelta(window time.Duration, now time.Time) uint64 {
	var total uint64
	for _, pod := range t.pods {
		total += pod.rxDelta(window, now)
	}
	return total
}

func (t procedureTraffic) windowReady(window time.Duration, now time.Time) bool {
	if len(t.pods) == 0 {
		return false
	}
	for _, pod := range t.pods {
		if !pod.windowReady(window, now) {
			return false
		}
	}
	return true
}

func (b *Backend) procedureTrafficForSelector(ctx context.Context, namespace string, selector map[string]string, now time.Time) (*procedureTraffic, error) {
	pods, err := b.activePodsForSelector(ctx, namespace, selector)
	if err != nil {
		return nil, err
	}
	return b.procedureTrafficForPods(ctx, pods, now)
}

func (b *Backend) procedureTrafficForJob(ctx context.Context, namespace string, jobName string, now time.Time) (*procedureTraffic, error) {
	pods, err := b.activePodsForJob(ctx, namespace, jobName)
	if err != nil {
		return nil, err
	}
	return b.procedureTrafficForPods(ctx, pods, now)
}

func (b *Backend) procedureTrafficForPods(ctx context.Context, pods []corev1.Pod, now time.Time) (*procedureTraffic, error) {
	if len(pods) == 0 {
		return nil, nil
	}
	result := &procedureTraffic{pods: make([]podTraffic, 0, len(pods))}
	for _, pod := range pods {
		traffic, err := b.podTraffic(ctx, pod, now)
		if err != nil {
			return nil, err
		}
		result.pods = append(result.pods, traffic)
		result.rxBytes += traffic.rxBytes
		result.txBytes += traffic.txBytes
		if traffic.lastActivityAt != nil && (result.lastActivityAt == nil || traffic.lastActivityAt.After(*result.lastActivityAt)) {
			last := *traffic.lastActivityAt
			result.lastActivityAt = &last
		}
	}
	return result, nil
}

func (b *Backend) activePodsForSelector(ctx context.Context, namespace string, selector map[string]string) ([]corev1.Pod, error) {
	if len(selector) == 0 {
		return nil, nil
	}
	list, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(selector).String()})
	if err != nil {
		return nil, err
	}
	return activePodItems(list.Items), nil
}

func (b *Backend) activePodsForJob(ctx context.Context, namespace string, jobName string) ([]corev1.Pod, error) {
	pods, err := b.podsForJobLabel(ctx, namespace, "batch.kubernetes.io/job-name", jobName)
	if err != nil {
		return nil, err
	}
	if len(pods) > 0 {
		return pods, nil
	}
	return b.podsForJobLabel(ctx, namespace, "job-name", jobName)
}

func (b *Backend) podsForJobLabel(ctx context.Context, namespace string, label string, jobName string) ([]corev1.Pod, error) {
	list, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(labels.Set{label: jobName}).String()})
	if err != nil {
		return nil, err
	}
	return activePodItems(list.Items), nil
}

func activePodItems(items []corev1.Pod) []corev1.Pod {
	pods := make([]corev1.Pod, 0, len(items))
	for _, pod := range items {
		if pod.DeletionTimestamp != nil || pod.Spec.NodeName == "" || pod.UID == "" {
			continue
		}
		if pod.Status.Phase != "" && pod.Status.Phase != corev1.PodRunning {
			continue
		}
		pods = append(pods, pod)
	}
	return pods
}

func (b *Backend) podTraffic(ctx context.Context, pod corev1.Pod, now time.Time) (podTraffic, error) {
	if pod.Spec.NodeName == "" {
		return podTraffic{}, fmt.Errorf("pod %s/%s has no assigned node", pod.Namespace, pod.Name)
	}
	if pod.UID == "" {
		return podTraffic{}, fmt.Errorf("pod %s/%s has no UID", pod.Namespace, pod.Name)
	}
	if b.statsReader == nil {
		return podTraffic{}, fmt.Errorf("kubernetes node stats reader is not configured")
	}
	summary, err := b.statsReader(ctx, pod.Spec.NodeName)
	if err != nil {
		return podTraffic{}, err
	}
	stats := summary.podByUID(pod.Namespace, string(pod.UID))
	if stats == nil {
		return podTraffic{}, fmt.Errorf("pod %s/%s uid %s not found in node stats", pod.Namespace, pod.Name, pod.UID)
	}
	rxBytes, txBytes, ok := stats.networkBytes()
	if !ok {
		return podTraffic{}, fmt.Errorf("pod %s/%s uid %s has no network stats", pod.Namespace, pod.Name, pod.UID)
	}
	traffic := globalPodTrafficStore.record(string(pod.UID), rxBytes, txBytes, now)
	return traffic, nil
}

func (s *nodeStatsSummary) podByUID(namespace string, uid string) *nodePodStats {
	if s == nil {
		return nil
	}
	for idx := range s.Pods {
		pod := &s.Pods[idx]
		if pod.PodRef.Namespace == namespace && pod.PodRef.UID == uid {
			return pod
		}
	}
	return nil
}

func (p *nodePodStats) networkBytes() (uint64, uint64, bool) {
	if p == nil || p.Network == nil {
		return 0, 0, false
	}
	if p.Network.RXBytes != nil || p.Network.TXBytes != nil {
		var rx uint64
		var tx uint64
		if p.Network.RXBytes != nil {
			rx = *p.Network.RXBytes
		}
		if p.Network.TXBytes != nil {
			tx = *p.Network.TXBytes
		}
		return rx, tx, true
	}
	var rx uint64
	var tx uint64
	var found bool
	for _, iface := range p.Network.Interfaces {
		if iface.RXBytes != nil {
			rx += *iface.RXBytes
			found = true
		}
		if iface.TXBytes != nil {
			tx += *iface.TXBytes
			found = true
		}
	}
	return rx, tx, found
}

func (b *Backend) readNodeStatsSummary(ctx context.Context, nodeName string) (*nodeStatsSummary, error) {
	if b.client == nil {
		return nil, fmt.Errorf("kubernetes client is not configured")
	}
	raw, err := b.client.CoreV1().RESTClient().
		Get().
		Resource("nodes").
		Name(nodeName).
		SubResource("proxy").
		Suffix("stats", "summary").
		Do(ctx).
		Raw()
	if err != nil {
		return nil, err
	}
	var summary nodeStatsSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}
