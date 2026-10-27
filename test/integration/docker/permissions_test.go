//go:build integration && docker

package docker_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/ports"
	dockerruntime "github.com/highcard-dev/daemon/internal/runtime/docker"
	"github.com/highcard-dev/daemon/test/integration/internal/e2e"
)

func TestDockerUpdatePreservesProtectedOwner(t *testing.T) {
	e2e.RequireDocker(t)
	name := fmt.Sprintf("druid-e2e-docker-volume-owner-%d", time.Now().UnixNano())
	baseImage := e2e.BuildDockerImage(t, "druid-cli-e2e:"+name)
	image := baseImage + "-nonroot"
	buildCtx, buildCancel := context.WithTimeout(context.Background(), time.Minute)
	defer buildCancel()
	build := exec.CommandContext(buildCtx, "docker", "build", "-t", image, "-")
	build.Stdin = strings.NewReader("FROM " + baseImage + "\nUSER 1000:1000\n")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("non-root worker fixture: %v: %s", err, output)
	}
	t.Cleanup(func() { e2e.Run(t, "docker", "image", "rm", image) })
	e2e.Run(t, "docker", "volume", "create", name)
	root := "docker-volume://" + name
	t.Cleanup(func() {
		// The only volume removed is this test's uniquely named fixture.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, "docker", "volume", "rm", name).CombinedOutput(); err != nil {
			t.Logf("fixture volume cleanup: %v: %s", err, output)
		}
	})
	yaml := "name: owner-fixture\nchunks:\n  - name: config\n    path: private.cfg\n    skip_update: true\n  - name: saves\n    path: saves\n    skip_update: true\n"
	candidate := t.TempDir()
	if err := os.WriteFile(filepath.Join(candidate, "scroll.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(candidate, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candidate, "data", "release.cfg"), []byte("new default"), 0600); err != nil {
		t.Fatal(err)
	}
	e2e.Run(t, "docker", "run", "--rm", "--user", "0", "-v", name+":/scroll", "--entrypoint", "sh", image, "-c", "mkdir -p /scroll/data/saves; printf '%s' \"$1\" > /scroll/scroll.yaml; printf fixture > /scroll/data/private.cfg; chmod 600 /scroll/data/private.cfg; chown -R 1000:1000 /scroll; printf saved > /scroll/data/saves/save; chmod 700 /scroll/data/saves; chmod 600 /scroll/data/saves/save; ln -s /not-mounted /scroll/data/saves/link; chown -hR 1234:2345 /scroll/data/saves", "fixture", yaml)
	backend, err := dockerruntime.NewWithConfig(dockerruntime.Config{WorkerImage: image}, ports.ProcedureStatusObserverFunc(func(ports.ProcedureStatusUpdate) {}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done, err := backend.SpawnPullWorker(ctx, ports.RuntimeWorkerAction{RuntimeID: name, RootRef: root, Artifact: candidate, Mode: ports.RuntimeWorkerModeUpdate})
	if err == nil {
		err = <-done
	}
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(e2e.Run(t, "docker", "run", "--rm", "--user", "0", "-v", name+":/scroll:ro", "--entrypoint", "stat", image, "-c", "%a:%u:%g", "/scroll/data/private.cfg"))
	if got != "600:1000:1000" {
		t.Errorf("update changed protected metadata: %s", got)
	}
	// Check actual access as the original owner, not just metadata inspection.
	content := e2e.Run(t, "docker", "run", "--rm", "--user", "1000:1000", "-v", name+":/scroll:ro", "--entrypoint", "cat", image, "/scroll/data/private.cfg")
	if content != "fixture" {
		t.Fatalf("protected content changed: %q", content)
	}
	for path, want := range map[string]string{"saves": "700:1234:2345", "saves/save": "600:1234:2345", "saves/link": "777:1234:2345", "release.cfg": "644:1000:1000"} {
		got := strings.TrimSpace(e2e.Run(t, "docker", "run", "--rm", "--user", "0", "-v", name+":/scroll:ro", "--entrypoint", "stat", image, "-c", "%a:%u:%g", "/scroll/data/"+path))
		if got != want {
			t.Errorf("%s metadata=%s, want %s", path, got, want)
		}
	}
	if got := e2e.Run(t, "docker", "run", "--rm", "--user", "1234:2345", "-v", name+":/scroll", "--entrypoint", "sh", image, "-c", "cat /scroll/data/saves/save; printf writable > /scroll/data/saves/new"); got != "saved" {
		t.Fatalf("custom-owner workload access: %q", got)
	}
	if got := e2e.Run(t, "docker", "run", "--rm", "--user", "1000:1000", "-v", name+":/scroll", "--entrypoint", "sh", image, "-c", "cat /scroll/data/release.cfg; printf writable > /scroll/data/new"); got != "new default" {
		t.Fatalf("default-owner workload access: %q", got)
	}
	t.Run("owner-failure-keeps-installed-root", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(candidate, "scroll.yaml"), []byte(strings.Replace(yaml, "owner-fixture", "rejected-fixture", 1)), 0644); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Deliberately remove chown capability only from this disposable worker.
		// It must fail before swapping in a tree with incorrect protected owners.
		out, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--user", "0", "--cap-drop", "CHOWN", "-v", name+":/scroll", "-v", candidate+":/candidate:ro", "--entrypoint", "druid", image, "worker", "pull", "--mode", "update", "--root", "/scroll", "--runtime-id", name, "--artifact", "/candidate").CombinedOutput()
		if err == nil || !strings.Contains(string(out), "set materialized path owner") {
			t.Fatalf("expected ownership failure: %v %s", err, out)
		}
		got := e2e.Run(t, "docker", "run", "--rm", "--user", "1000:1000", "-v", name+":/scroll:ro", "--entrypoint", "cat", image, "/scroll/scroll.yaml")
		if got != yaml {
			t.Fatal("failed ownership preservation replaced installed YAML")
		}
		if got := e2e.Run(t, "docker", "run", "--rm", "--user", "1234:2345", "-v", name+":/scroll:ro", "--entrypoint", "cat", image, "/scroll/data/saves/new"); got != "writable" {
			t.Fatal("failed ownership preservation changed installed data/access")
		}
	})
}
