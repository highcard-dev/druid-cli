//go:build integration && docker

package docker_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/test/integration/internal/e2e"
)

func TestDockerBackendCLIComplexLifecycle(t *testing.T) {
	e2e.RequireDocker(t)
	bins := e2e.BuildBinaries(t)
	port := e2e.FreePort(t)
	routePort := e2e.FreePort(t)
	callbackPort := e2e.FreePort(t)
	name := fmt.Sprintf("docker-cli-%d", time.Now().UnixNano())
	fixture := e2e.WriteFixture(t, filepath.Join(t.TempDir(), "scroll"), name, port, routePort)
	workerImage := e2e.BuildDockerImage(t, "druid-cli-e2e:"+name)
	containerHost := e2e.DockerHostAddress(t)

	socket := filepath.Join(os.TempDir(), fmt.Sprintf("druid-docker-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(socket) })
	stateDir := filepath.Join(t.TempDir(), "state")
	logs := e2e.StartDaemon(t, bins, "docker", socket, stateDir, []string{
		"--docker-worker-image", workerImage,
		"--docker-storage", "bind",
		"--docker-bind-root", filepath.Join(stateDir, "scrolls"),
		"--worker-callback-listen", fmt.Sprintf(":%d", callbackPort),
		"--worker-callback-url", fmt.Sprintf("http://%s:%d", containerHost, callbackPort),
	}, nil)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("druid daemon logs:\n%s", logs.String())
		}
	})

	created := e2e.RunClientJSON[e2e.RuntimeScroll](t, bins, socket, "create", "-p", fmt.Sprintf("%d:http", fixture.RoutePort), fixture.Dir, fixture.Name)
	if created.Status != "created" {
		t.Fatalf("created status = %s, want created", created.Status)
	}

	started := e2e.RunClientJSON[e2e.RuntimeScroll](t, bins, socket, "start", created.ID)
	if started.Status != "running" {
		t.Fatalf("started status = %s, want running", started.Status)
	}
	body := e2e.WaitHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/env.txt", fixture.RoutePort))
	env := e2e.ParseEnv(body)
	e2e.AssertRuntimeEnv(t, env, fixture, "docker", created.ID)
	if env["USER_ENV"] != "fixture" {
		t.Fatalf("USER_ENV = %q, want fixture", env["USER_ENV"])
	}

	statuses := e2e.RunClientJSON[[]e2e.RuntimePortStatus](t, bins, socket, "ports", created.ID)
	assertPortBound(t, statuses, fixture)

	e2e.UnixJSONRequest(t, socket, http.MethodPost, "/api/v1/scrolls/"+created.ID+"/commands/record", "")
	root := strings.TrimPrefix(created.Root, "docker-bind://")
	if got := readDockerRootFile(t, root, "data/finite.txt"); !strings.Contains(got, "finite-ok") {
		t.Fatalf("finite file = %q, want finite-ok", got)
	}
	recordEnv := e2e.ParseEnv(readDockerRootFile(t, root, "data/record-env.txt"))
	e2e.AssertRuntimeEnv(t, recordEnv, fixture, "docker", created.ID)
	if recordEnv["USER_ENV"] != "finite" {
		t.Fatalf("record USER_ENV = %q, want finite", recordEnv["USER_ENV"])
	}

	e2e.RunClient(t, bins, socket, "stop", created.ID)
	waitDockerContainersGone(t, fixture.ServeProc, fixture.RecordProc)
	deleted := e2e.UnixJSONRequest(t, socket, http.MethodDelete, "/api/v1/scrolls/"+created.ID+"?purge_data=true", "")
	if !strings.Contains(deleted, `"status":"deleted"`) {
		t.Fatalf("delete response = %s, want deleted status", deleted)
	}
}

func TestDockerBackendVolumeStorageWorkerLifecycleBackupRestore(t *testing.T) {
	e2e.RequireDocker(t)
	bins := e2e.BuildBinaries(t)
	port := e2e.FreePort(t)
	routePort := e2e.FreePort(t)
	callbackPort := e2e.FreePort(t)
	publicPort := e2e.FreePort(t)
	managementPort := e2e.FreePort(t)
	registryPort := e2e.StartRegistry(t)
	containerHost := e2e.DockerHostAddress(t)
	name := fmt.Sprintf("docker-volume-%d", time.Now().UnixNano())
	fixture := e2e.WriteFixture(t, filepath.Join(t.TempDir(), "scroll"), name, port, routePort)
	workerImage := e2e.BuildDockerImage(t, "druid-cli-e2e:"+name)

	pushArtifact := fmt.Sprintf("127.0.0.1:%d/druid-e2e/%s:v1", registryPort, name)
	runtimeArtifact := fmt.Sprintf("%s:%d/druid-e2e/%s:v1", containerHost, registryPort, name)
	backupArtifact := fmt.Sprintf("%s:%d/druid-e2e/%s-backup:v1", containerHost, registryPort, name)
	e2e.RunEnv(t, []string{"DRUID_REGISTRY_PLAIN_HTTP=true", "HOME=" + bins.Home}, bins.Druid, "push", pushArtifact, fixture.Dir)

	socket := filepath.Join(os.TempDir(), fmt.Sprintf("druid-docker-volume-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(socket) })
	stateDir := filepath.Join(t.TempDir(), "state")
	logs := e2e.StartDaemon(t, bins, "docker", socket, stateDir, []string{
		"--docker-worker-image", workerImage,
		"--docker-volume-prefix", "druid-e2e",
		"--worker-callback-listen", fmt.Sprintf(":%d", callbackPort),
		"--worker-callback-url", fmt.Sprintf("http://%s:%d", containerHost, callbackPort),
		"--listen", fmt.Sprintf(":%d", managementPort),
		"--worker-daemon-url", fmt.Sprintf("http://%s:%d", containerHost, managementPort),
		"--unsafe-allow-unauthenticated-management",
	}, []string{"DRUID_REGISTRY_PLAIN_HTTP=true"})
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("druid daemon logs:\n%s", logs.String())
		}
	})

	created := e2e.RunClientJSON[e2e.RuntimeScroll](t, bins, socket,
		"create",
		"-p", fmt.Sprintf("%d:http", fixture.RoutePort),
		"-p", fmt.Sprintf("%d:webdav", publicPort),
		runtimeArtifact,
		fixture.Name,
	)
	if created.Status != "created" {
		t.Fatalf("created status = %s, want created", created.Status)
	}
	if !strings.HasPrefix(created.Root, "docker-volume://druid-e2e-") {
		t.Fatalf("root = %s, want docker volume ref", created.Root)
	}
	started := e2e.RunClientJSON[e2e.RuntimeScroll](t, bins, socket, "start", created.ID)
	if started.Status != "running" {
		t.Fatalf("started status = %s, want running", started.Status)
	}
	body := e2e.WaitHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/env.txt", fixture.RoutePort))
	env := e2e.ParseEnv(body)
	e2e.AssertRuntimeEnv(t, env, fixture, "docker", created.ID)

	e2e.RunClient(t, bins, socket, "dev", created.ID, "--watch", "data", "--command", "record")

	finiteURL := fmt.Sprintf("http://127.0.0.1:%d/webdav/data/finite.txt", publicPort)
	if got := e2e.WaitHTTP(t, finiteURL); !strings.Contains(got, "finite-ok") {
		t.Fatalf("finite file = %q, want finite-ok", got)
	}

	e2e.UnixJSONRequest(t, socket, http.MethodPost, "/api/v1/scrolls/"+created.ID+"/backup", fmt.Sprintf(`{"artifact":%q}`, backupArtifact))
	indexURL := fmt.Sprintf("http://127.0.0.1:%d/webdav/data/public/index.txt", publicPort)
	httpPut(t, indexURL, "mutated\n")
	if got := e2e.WaitHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/index.txt", fixture.RoutePort)); !strings.Contains(got, "mutated") {
		t.Fatalf("mutated index = %q, want mutated", got)
	}

	e2e.UnixJSONRequest(t, socket, http.MethodPost, "/api/v1/scrolls/"+created.ID+"/restore", fmt.Sprintf(`{"artifact":%q,"restart":true}`, backupArtifact))
	if got := e2e.WaitHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/index.txt", fixture.RoutePort)); !strings.Contains(got, "healthy") {
		t.Fatalf("restored index = %q, want healthy", got)
	}
	e2e.UnixJSONRequest(t, socket, http.MethodDelete, "/api/v1/scrolls/"+created.ID+"?purge_data=true", "")
}

func TestDockerBackendColdstarterFrontsRuntime(t *testing.T) {
	e2e.RequireDocker(t)
	bins := e2e.BuildBinaries(t)
	runtimePort := e2e.FreePort(t)
	publicPort := e2e.FreePort(t)
	callbackPort := e2e.FreePort(t)
	name := fmt.Sprintf("docker-coldstart-%d", time.Now().UnixNano())
	image := e2e.BuildDockerImage(t, "druid-coldstart-e2e:"+name)
	fixtureDir := writeColdstarterFixture(t, filepath.Join(t.TempDir(), "scroll"), name, image, runtimePort)
	containerHost := e2e.DockerHostAddress(t)

	socket := filepath.Join(os.TempDir(), fmt.Sprintf("druid-coldstart-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(socket) })
	stateDir := filepath.Join(t.TempDir(), "state")
	logs := e2e.StartDaemon(t, bins, "docker", socket, stateDir, []string{
		"--docker-worker-image", image,
		"--docker-storage", "bind",
		"--docker-bind-root", filepath.Join(stateDir, "scrolls"),
		"--worker-callback-listen", fmt.Sprintf(":%d", callbackPort),
		"--worker-callback-url", fmt.Sprintf("http://%s:%d", containerHost, callbackPort),
	}, nil)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("druid daemon logs:\n%s", logs.String())
		}
	})

	created := e2e.RunClientJSON[e2e.RuntimeScroll](t, bins, socket, "create", "-p", fmt.Sprintf("%d:http", publicPort), fixtureDir, name)
	if created.Status != "created" {
		t.Fatalf("created status = %s, want created", created.Status)
	}
	started := e2e.RunClientJSON[e2e.RuntimeScroll](t, bins, socket, "start", created.ID)
	if started.Status != "running" {
		t.Fatalf("started status = %s, want running", started.Status)
	}
	queue := readProcedureStatusMap(t, socket, "/api/v1/scrolls/"+created.ID+"/queue")
	if queue["start"]["coldstart"].Status != "running" {
		t.Fatalf("queue = %#v, want start/coldstart running", queue)
	}
	if got := e2e.WaitHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/index.txt", publicPort)); !strings.Contains(got, "cold-started") {
		t.Fatalf("served body = %q, want cold-started", got)
	}
	e2e.UnixJSONRequest(t, socket, http.MethodDelete, "/api/v1/scrolls/"+created.ID+"?purge_data=true", "")
}

type lockStatus struct {
	Status string `json:"status"`
}

func readProcedureStatusMap(t *testing.T, socket string, path string) map[string]map[string]lockStatus {
	t.Helper()
	body := e2e.UnixJSONRequest(t, socket, http.MethodGet, path, "")
	statuses := map[string]map[string]lockStatus{}
	if err := json.Unmarshal([]byte(body), &statuses); err != nil {
		t.Fatalf("decode %s JSON: %v\n%s", path, err, body)
	}
	return statuses
}

func assertPortBound(t *testing.T, statuses []e2e.RuntimePortStatus, fixture e2e.Fixture) {
	t.Helper()
	for _, status := range statuses {
		if status.Name == "http" && status.Procedure == fixture.ServeProc {
			if !status.Bound {
				t.Fatalf("port status = %#v, want bound", status)
			}
			if status.HostPort != fixture.RoutePort {
				t.Fatalf("host port = %d, want %d in status %#v", status.HostPort, fixture.RoutePort, status)
			}
			return
		}
	}
	t.Fatalf("http port for %s not found in %#v", fixture.ServeProc, statuses)
}

func writeColdstarterFixture(t *testing.T, dir string, name string, image string, port int) string {
	t.Helper()
	yaml := fmt.Sprintf(`name: %s
desc: Coldstarter integration fixture
version: 0.1.0
app_version: "test"
serve: start
ports:
  - name: http
    protocol: http
    port: %d
commands:
  start:
    run: restart
    procedures:
      - id: coldstart
        image: %s
        expectedPorts:
          - name: http
            keepAliveTraffic: 1b/5m
        mounts:
          - path: /runtime
            sub_path: .
        env:
          DRUID_ROOT: /runtime
          DRUID_PORT_HTTP_COLDSTARTER: generic
        command:
          - druid-coldstarter
      - id: web
        image: busybox:1.36
        expectedPorts:
          - name: http
            keepAliveTraffic: 1b/5m
        mounts:
          - path: /site
            sub_path: public
        command:
          - sh
          - -c
          - >-
            set -eu;
            mkdir -p /site;
            printf 'cold-started\n' > /site/index.txt;
            httpd -f -p %d -h /site
`, name, port, image, port)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scroll.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readDockerRootFile(t *testing.T, root string, path string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		out := e2e.Run(t, "docker", "run", "--rm", "-v", root+":/runtime:ro", "busybox:1.36", "sh", "-c", "cat /runtime/"+path+" 2>&1 || true")
		if !strings.Contains(out, "No such file") {
			return out
		}
		last = out
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("read docker root %s:%s: %s", root, path, last)
	return ""
}

func waitDockerContainersGone(t *testing.T, labels ...string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		for _, label := range labels {
			out := e2e.Run(t, "docker", "ps", "-a", "--filter", "label=druid.command="+label, "--format", "{{.Names}}")
			if strings.TrimSpace(out) != "" {
				found = true
			}
		}
		if !found {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("docker containers still exist for labels %v", labels)
}

func httpPut(t *testing.T, url string, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT %s failed with %d: %s", url, resp.StatusCode, data)
	}
}
