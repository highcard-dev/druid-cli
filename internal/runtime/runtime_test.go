package runtime_test

import (
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
