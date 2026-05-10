//go:build integration && kubernetes

package kubernetes_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/test/integration/internal/e2e"
)

func TestKubernetesBackendCLIComplexLifecycle(t *testing.T) {
	requireKubernetes(t)
	bins := e2e.BuildBinaries(t)
	port := e2e.FreePort(t)
	routePort := e2e.FreePort(t)
	suffix := fmt.Sprintf("%x", time.Now().UnixNano())[:10]
	namespace := "druid-cli-e2e-" + suffix
	pvc := "druid-e2e-" + suffix
	ref := fmt.Sprintf("k8s://%s/%s", namespace, pvc)
	name := "k8s-cli-" + suffix
	fixture := e2e.WriteFixture(t, filepath.Join(t.TempDir(), "scroll"), name, port, routePort)

	e2e.Run(t, "kubectl", "create", "namespace", namespace)
	t.Cleanup(func() {
		e2e.Run(t, "kubectl", "delete", "namespace", namespace, "--ignore-not-found=true", "--wait=false")
	})
	seedPVC(t, namespace, pvc, fixture.Dir)
	kubeconfig := writeCurrentKubeconfig(t)

	socket := filepath.Join(os.TempDir(), fmt.Sprintf("druid-k8s-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(socket) })
	stateDir := filepath.Join(t.TempDir(), "state")
	logs := e2e.StartDaemon(t, bins, "kubernetes", socket, stateDir, []string{
		"--k8s-namespace", namespace,
		"--k8s-kubeconfig", kubeconfig,
		"--hubble-relay-addr", "127.0.0.1:9",
	}, nil)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("druid daemon logs:\n%s", logs.String())
		}
	})

	created := e2e.RunClientJSON[e2e.RuntimeScroll](t, bins, socket, "create", "--no-start", "--scroll-root", ref, "--data-root", ref, "seeded-artifact", fixture.Name)
	if created.Status != "created" {
		t.Fatalf("created status = %s, want created", created.Status)
	}
	targets := e2e.RunClientJSON[[]e2e.RuntimeRoutingTarget](t, bins, socket, "routing", "targets", created.ID)
	target := findTarget(t, targets, fixture)
	if target.Namespace != namespace || target.ServicePort != fixture.Port {
		t.Fatalf("target = %#v, want namespace %s service port %d", target, namespace, fixture.Port)
	}

	e2e.RunClient(t, bins, socket, "routing", "apply", created.ID, "--file", fixture.RoutingFile)
	started := e2e.RunClientJSON[e2e.RuntimeScroll](t, bins, socket, "start", created.ID)
	if started.Status != "running" {
		t.Fatalf("started status = %s, want running", started.Status)
	}

	localPort := e2e.FreePort(t)
	waitServiceExists(t, namespace, target.ServiceName)
	waitRuntimePodReady(t, namespace, pvc)
	forward := startPortForward(t, namespace, target.ServiceName, localPort, fixture.Port)
	t.Cleanup(forward)
	body := e2e.WaitHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/env.txt", localPort))
	env := e2e.ParseEnv(body)
	e2e.AssertRuntimeEnv(t, env, fixture, "kubernetes", created.ID)
	if env["USER_ENV"] != "fixture" {
		t.Fatalf("USER_ENV = %q, want fixture", env["USER_ENV"])
	}

	statuses := e2e.RunClientJSON[[]e2e.RuntimePortStatus](t, bins, socket, "ports", created.ID)
	assertKubernetesPort(t, statuses, fixture)

	e2e.RunClient(t, bins, socket, "run", created.ID, "record")
	if got := readPVCFile(t, namespace, pvc, "data/finite.txt"); !strings.Contains(got, "finite-ok") {
		t.Fatalf("finite file = %q, want finite-ok", got)
	}
	recordEnv := e2e.ParseEnv(readPVCFile(t, namespace, pvc, "data/record-env.txt"))
	e2e.AssertRuntimeEnv(t, recordEnv, fixture, "kubernetes", created.ID)
	if recordEnv["USER_ENV"] != "finite" {
		t.Fatalf("record USER_ENV = %q, want finite", recordEnv["USER_ENV"])
	}

	stopped := e2e.RunClientJSON[e2e.RuntimeScroll](t, bins, socket, "stop", created.ID)
	if stopped.Status != "stopped" {
		t.Fatalf("stopped status = %s, want stopped", stopped.Status)
	}
	waitKubernetesResourcesGone(t, namespace, pvc, "statefulset,job,pod")
	deleted := e2e.RunClient(t, bins, socket, "delete", created.ID)
	if !strings.Contains(deleted, `"status": "deleted"`) {
		t.Fatalf("delete response = %s, want deleted status", deleted)
	}
	waitKubernetesResourcesGone(t, namespace, pvc, "service")
}

func requireKubernetes(t *testing.T) {
	t.Helper()
	e2e.RequireCommand(t, "kubectl")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", "cluster-info")
	if out, err := cmd.CombinedOutput(); err != nil {
		if os.Getenv("CI") == "" {
			t.Skipf("Kubernetes cluster is required for this integration test: %v\n%s", err, out)
		}
		t.Fatalf("Kubernetes cluster is required for this integration test: %v\n%s", err, out)
	}
}

func seedPVC(t *testing.T, namespace string, pvc string, fixtureDir string) {
	t.Helper()
	applyManifest(t, fmt.Sprintf(`apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %s
  namespace: %s
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
---
apiVersion: v1
kind: Pod
metadata:
  name: seed-%s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
    - name: seed
      image: busybox:1.36
      command: ["sh", "-c", "sleep 3600"]
      volumeMounts:
        - name: runtime
          mountPath: /runtime
  volumes:
    - name: runtime
      persistentVolumeClaim:
        claimName: %s
`, pvc, namespace, pvc, namespace, pvc))
	seedPod := "seed-" + pvc
	e2e.Run(t, "kubectl", "wait", "-n", namespace, "--for=condition=Ready", "pod/"+seedPod, "--timeout=180s")
	e2e.Run(t, "kubectl", "cp", filepath.Join(fixtureDir, "scroll.yaml"), namespace+"/"+seedPod+":/runtime/scroll.yaml")
	e2e.Run(t, "kubectl", "exec", "-n", namespace, seedPod, "--", "sh", "-c", "mkdir -p /runtime/data/public")
	e2e.Run(t, "kubectl", "delete", "pod", "-n", namespace, seedPod, "--wait=true")
}

func applyManifest(t *testing.T, manifest string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	e2e.Run(t, "kubectl", "apply", "-f", path)
}

func writeCurrentKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	data := e2e.Run(t, "kubectl", "config", "view", "--raw", "--minify", "--flatten")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func findTarget(t *testing.T, targets []e2e.RuntimeRoutingTarget, fixture e2e.Fixture) e2e.RuntimeRoutingTarget {
	t.Helper()
	for _, target := range targets {
		if target.PortName == "http" && target.Procedure == fixture.ServeProc {
			return target
		}
	}
	t.Fatalf("http target for %s not found in %#v", fixture.ServeProc, targets)
	return e2e.RuntimeRoutingTarget{}
}

func startPortForward(t *testing.T, namespace string, service string, localPort int, remotePort int) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "kubectl", "port-forward", "-n", namespace, "svc/"+service, fmt.Sprintf("%d:%d", localPort, remotePort))
	var logs e2e.LockedBuffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start kubectl port-forward: %v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(logs.String(), "Forwarding from") {
			return func() {
				cancel()
				_ = cmd.Wait()
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	_ = cmd.Wait()
	t.Fatalf("kubectl port-forward did not become ready:\n%s", logs.String())
	return func() {}
}

func waitServiceExists(t *testing.T, namespace string, service string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := kubectlOutput("get", "service", "-n", namespace, service); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	out, _ := kubectlOutput("get", "service", "-n", namespace, "-o", "name")
	t.Fatalf("service %s did not appear; services:\n%s", service, out)
}

func waitRuntimePodReady(t *testing.T, namespace string, pvc string) {
	t.Helper()
	selector := "app.kubernetes.io/managed-by=druid,druid.gg/scroll-id=" + pvc
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		out, err := kubectlOutput("get", "pod", "-n", namespace, "-l", selector, "-o", "jsonpath={.items[0].status.phase}")
		if err == nil && strings.TrimSpace(out) == "Running" {
			if _, waitErr := kubectlOutput("wait", "-n", namespace, "--for=condition=Ready", "pod", "-l", selector, "--timeout=10s"); waitErr == nil {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	out, _ := kubectlOutput("get", "pod", "-n", namespace, "-l", selector, "-o", "wide")
	t.Fatalf("runtime pod did not become ready:\n%s", out)
}

func assertKubernetesPort(t *testing.T, statuses []e2e.RuntimePortStatus, fixture e2e.Fixture) {
	t.Helper()
	for _, status := range statuses {
		if status.Name == "http" && status.Procedure == fixture.ServeProc {
			if !status.Bound {
				t.Fatalf("port status = %#v, want bound", status)
			}
			if status.Port != fixture.Port {
				t.Fatalf("service port = %d, want %d in status %#v", status.Port, fixture.Port, status)
			}
			return
		}
	}
	t.Fatalf("http port for %s not found in %#v", fixture.ServeProc, statuses)
}

func readPVCFile(t *testing.T, namespace string, pvc string, relativePath string) string {
	t.Helper()
	pod := "read-" + strings.ReplaceAll(relativePath, "/", "-") + "-" + fmt.Sprintf("%x", time.Now().UnixNano())[:8]
	applyManifest(t, fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
    - name: read
      image: busybox:1.36
      command: ["sh", "-c", "cat /runtime/%s"]
      volumeMounts:
        - name: runtime
          mountPath: /runtime
  volumes:
    - name: runtime
      persistentVolumeClaim:
        claimName: %s
`, pod, namespace, relativePath, pvc))
	defer e2e.Run(t, "kubectl", "delete", "pod", "-n", namespace, pod, "--ignore-not-found=true", "--wait=false")
	waitPodSucceeded(t, namespace, pod)
	return e2e.Run(t, "kubectl", "logs", "-n", namespace, pod)
}

func waitPodSucceeded(t *testing.T, namespace string, pod string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		out, err := kubectlOutput("get", "pod", "-n", namespace, pod, "-o", "jsonpath={.status.phase}")
		phase := strings.TrimSpace(out)
		if err == nil && phase == "Succeeded" {
			return
		}
		if err == nil && phase == "Failed" {
			logs, _ := kubectlOutput("logs", "-n", namespace, pod)
			t.Fatalf("pod %s failed:\n%s", pod, logs)
		}
		time.Sleep(500 * time.Millisecond)
	}
	logs, _ := kubectlOutput("logs", "-n", namespace, pod)
	t.Fatalf("pod %s did not succeed:\n%s", pod, logs)
}

func kubectlOutput(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func waitKubernetesResourcesGone(t *testing.T, namespace string, pvc string, resource string) {
	t.Helper()
	selector := "app.kubernetes.io/managed-by=druid,druid.gg/scroll-id=" + pvc
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		out := e2e.Run(t, "kubectl", "get", resource, "-n", namespace, "-l", selector, "-o", "name", "--ignore-not-found=true")
		if strings.TrimSpace(out) == "" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	out := e2e.Run(t, "kubectl", "get", resource, "-n", namespace, "-l", selector, "-o", "name", "--ignore-not-found=true")
	t.Fatalf("kubernetes %s still exist:\n%s", resource, out)
}
