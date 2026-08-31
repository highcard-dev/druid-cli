package kubernetes

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSelectConsolePodPrefersRunningThenNewest(t *testing.T) {
	selected := selectConsolePod([]corev1.Pod{
		consolePod("old-stopped", corev1.PodSucceeded, 1),
		consolePod("running", corev1.PodRunning, 2),
		consolePod("new-stopped", corev1.PodSucceeded, 3),
	})
	if selected == nil || selected.Name != "running" {
		t.Fatalf("selected = %#v, want running", selected)
	}

	selected = selectConsolePod([]corev1.Pod{
		consolePod("old", corev1.PodSucceeded, 1),
		consolePod("new", corev1.PodSucceeded, 2),
	})
	if selected == nil || selected.Name != "new" {
		t.Fatalf("selected = %#v, want new", selected)
	}
}

func TestSelectProcedureConsoleFindsRunningPersistentInitContainer(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "minecraft-start-1-0",
			Labels: map[string]string{labelProcedure: "start"},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init-coldstart"}},
			Containers:     []corev1.Container{{Name: "main"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name:  "init-coldstart",
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}},
		},
	}
	selected, container, running := selectProcedureConsole([]corev1.Pod{pod}, "coldstart")
	if selected == nil || selected.Name != pod.Name || container != "init-coldstart" || !running {
		t.Fatalf("selected = %#v, container = %q, running = %v", selected, container, running)
	}
	selected, container, running = selectProcedureConsole([]corev1.Pod{pod}, "start")
	if selected == nil || container != "main" || running {
		t.Fatalf("selected = %#v, container = %q, running = %v", selected, container, running)
	}
}

func consolePod(name string, phase corev1.PodPhase, created int64) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(time.Unix(created, 0))},
		Status:     corev1.PodStatus{Phase: phase},
	}
}
