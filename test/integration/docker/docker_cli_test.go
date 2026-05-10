//go:build integration && docker

package docker_test

import (
	"fmt"
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
	name := fmt.Sprintf("docker-cli-%d", time.Now().UnixNano())
	fixture := e2e.WriteFixture(t, filepath.Join(t.TempDir(), "scroll"), name, port, routePort)

	socket := filepath.Join(os.TempDir(), fmt.Sprintf("druid-docker-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(socket) })
	stateDir := filepath.Join(t.TempDir(), "state")
	logs := e2e.StartDaemon(t, bins, "docker", socket, stateDir, nil, nil)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("druid daemon logs:\n%s", logs.String())
		}
	})

	created := e2e.RunClientJSON[e2e.RuntimeScroll](t, bins, socket, "create", "--no-start", "--state-dir", stateDir, fixture.Dir, fixture.Name)
	if created.Status != "created" {
		t.Fatalf("created status = %s, want created", created.Status)
	}
	e2e.RunClient(t, bins, socket, "routing", "apply", created.ID, "--file", fixture.RoutingFile)

	started := e2e.RunClientJSON[e2e.RuntimeScroll](t, bins, socket, "start", created.ID)
	if started.Status != "running" {
		t.Fatalf("started status = %s, want running", started.Status)
	}
	body := e2e.WaitHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/env.txt", fixture.Port))
	env := e2e.ParseEnv(body)
	e2e.AssertRuntimeEnv(t, env, fixture, "docker", created.ID)
	if env["USER_ENV"] != "fixture" {
		t.Fatalf("USER_ENV = %q, want fixture", env["USER_ENV"])
	}

	statuses := e2e.RunClientJSON[[]e2e.RuntimePortStatus](t, bins, socket, "ports", created.ID)
	assertPortBound(t, statuses, fixture)

	e2e.RunClient(t, bins, socket, "run", created.ID, "record")
	dataRoot := filepath.Join(stateDir, "scrolls", created.ID, "data")
	assertFileContains(t, filepath.Join(dataRoot, "finite.txt"), "finite-ok")
	recordEnv := e2e.ParseEnv(readFile(t, filepath.Join(dataRoot, "record-env.txt")))
	e2e.AssertRuntimeEnv(t, recordEnv, fixture, "docker", created.ID)
	if recordEnv["USER_ENV"] != "finite" {
		t.Fatalf("record USER_ENV = %q, want finite", recordEnv["USER_ENV"])
	}

	e2e.RunClient(t, bins, socket, "stop", created.ID)
	waitDockerContainersGone(t, fixture.ServeProc, fixture.RecordProc)
	deleted := e2e.RunClient(t, bins, socket, "delete", created.ID)
	if !strings.Contains(deleted, `"status": "deleted"`) {
		t.Fatalf("delete response = %s, want deleted status", deleted)
	}
}

func assertPortBound(t *testing.T, statuses []e2e.RuntimePortStatus, fixture e2e.Fixture) {
	t.Helper()
	for _, status := range statuses {
		if status.Name == "http" && status.Procedure == fixture.ServeProc {
			if !status.Bound {
				t.Fatalf("port status = %#v, want bound", status)
			}
			if status.HostPort != fixture.Port {
				t.Fatalf("host port = %d, want %d in status %#v", status.HostPort, fixture.Port, status)
			}
			return
		}
	}
	t.Fatalf("http port for %s not found in %#v", fixture.ServeProc, statuses)
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	got := readFile(t, path)
	if !strings.Contains(got, want) {
		t.Fatalf("%s = %q, want to contain %q", path, got, want)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
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
