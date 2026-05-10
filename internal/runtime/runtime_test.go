package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/runtime/docker"
)

func TestDockerRunCommandBuildsCanonicalMounts(t *testing.T) {
	dataRoot := t.TempDir()
	procedure := &domain.Procedure{
		Image:      "alpine:3.20",
		Command:    []string{"sh", "-c", "echo ok"},
		WorkingDir: "/cache",
		Env: map[string]string{
			"B": "two",
			"A": "one",
		},
		ExpectedPorts: []domain.ExpectedPort{{Name: "http"}},
		Mounts:        []domain.Mount{{Path: "/cache", SubPath: "cache"}},
	}

	spec, err := docker.BuildContainerSpec("start", procedure, dataRoot, []domain.Port{{Name: "http", Port: 8080, Protocol: "http"}})
	if err != nil {
		t.Fatal(err)
	}

	if spec.Image != "alpine:3.20" {
		t.Fatalf("unexpected image: %s", spec.Image)
	}
	if !reflect.DeepEqual(spec.Command, []string{"sh", "-c", "echo ok"}) {
		t.Fatalf("unexpected command: %#v", spec.Command)
	}
	if spec.WorkingDir != "/cache" {
		t.Fatalf("unexpected working dir: %s", spec.WorkingDir)
	}
	if !reflect.DeepEqual(spec.Env, []string{"A=one", "B=two"}) {
		t.Fatalf("unexpected env: %#v", spec.Env)
	}
	expectedBinds := []string{
		filepath.Join(dataRoot, "data", "cache") + ":/cache",
	}
	if !reflect.DeepEqual(spec.Binds, expectedBinds) {
		t.Fatalf("unexpected binds:\nexpected: %#v\nactual:   %#v", expectedBinds, spec.Binds)
	}
	if len(spec.PortBindings) != 1 {
		t.Fatalf("expected one port binding, got %#v", spec.PortBindings)
	}

	if _, err := os.Stat(filepath.Join(dataRoot, "data", "cache")); err != nil {
		t.Fatalf("expected mount subpath to be created: %v", err)
	}
}

func TestDockerBuildContainerSpecUsesProvidedRuntimeEnv(t *testing.T) {
	dataRoot := t.TempDir()
	spec, err := docker.BuildContainerSpecWithEnv("start", &domain.Procedure{
		Image: "alpine:3.20",
		Env: map[string]string{
			"PROCEDURE_ONLY": "ignored",
		},
	}, dataRoot, nil, map[string]string{
		"DRUID_PORT_HTTP": "8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec.Env, []string{"DRUID_PORT_HTTP=8080"}) {
		t.Fatalf("env = %#v", spec.Env)
	}
}

func TestDockerRunCommandDefaultsMountSubPathToDataRoot(t *testing.T) {
	dataRoot := t.TempDir()
	spec, err := docker.BuildContainerSpec("start", &domain.Procedure{
		Image:   "alpine:3.20",
		Mounts:  []domain.Mount{{Path: "/server"}},
		Command: []string{"true"},
	}, dataRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectedBinds := []string{filepath.Join(dataRoot, "data") + ":/server"}
	if !reflect.DeepEqual(spec.Binds, expectedBinds) {
		t.Fatalf("unexpected binds:\nexpected: %#v\nactual:   %#v", expectedBinds, spec.Binds)
	}
}

func TestDockerBuildContainerSpecSupportsTTY(t *testing.T) {
	dataRoot := t.TempDir()

	spec, err := docker.BuildContainerSpec("build.0", &domain.Procedure{
		Image:      domain.DefaultExecImage,
		Command:    []string{"bash", "-lc", "echo ok"},
		WorkingDir: "/work",
		TTY:        true,
	}, dataRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !spec.TTY {
		t.Fatal("expected TTY enabled")
	}
	if spec.Image != domain.DefaultExecImage {
		t.Fatalf("unexpected image: %s", spec.Image)
	}
}

func TestDockerReadScrollFile(t *testing.T) {
	scrollRoot := t.TempDir()
	want := []byte("name: test\n")
	if err := os.WriteFile(filepath.Join(scrollRoot, "scroll.yaml"), want, 0644); err != nil {
		t.Fatal(err)
	}
	backend := &docker.Backend{}
	got, err := backend.ReadScrollFile(scrollRoot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("scroll yaml = %q, want %q", got, want)
	}
}

func TestDockerReadDataFileScopesToDataRoot(t *testing.T) {
	dataRoot := t.TempDir()
	want := []byte("bundle")
	path := filepath.Join(dataRoot, "data", "private", "dist")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "app.wasm"), want, 0644); err != nil {
		t.Fatal(err)
	}
	backend := &docker.Backend{}
	got, err := backend.ReadDataFile(context.Background(), dataRoot, "/data/private/dist/app.wasm")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("data file = %q, want %q", got, want)
	}
	if _, err := backend.ReadDataFile(context.Background(), dataRoot, "../escape"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestDockerWriteDataFileScopesToDataRoot(t *testing.T) {
	dataRoot := t.TempDir()
	backend := &docker.Backend{}
	if err := backend.WriteDataFile(context.Background(), dataRoot, "data/private/config.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dataRoot, "data", "private", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{}" {
		t.Fatalf("written data = %q, want {}", got)
	}
	if err := backend.WriteDataFile(context.Background(), dataRoot, "../escape", []byte("bad")); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}
