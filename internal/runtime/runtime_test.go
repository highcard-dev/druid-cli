package runtime_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/runtime/docker"
)

func TestDockerRunCommandBuildsCanonicalMounts(t *testing.T) {
	root := t.TempDir()
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

	spec, err := docker.BuildContainerSpec("start", procedure, root, []domain.Port{{Name: "http", Port: 8080, Protocol: "http"}})
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
	expectedMounts := []mount.Mount{
		{
			Type:        mount.TypeBind,
			Source:      filepath.Join(root, "data", "cache"),
			Target:      "/cache",
			BindOptions: &mount.BindOptions{CreateMountpoint: true},
		},
	}
	if !reflect.DeepEqual(spec.Mounts, expectedMounts) {
		t.Fatalf("unexpected mounts:\nexpected: %#v\nactual:   %#v", expectedMounts, spec.Mounts)
	}
	if len(spec.PortBindings) != 1 {
		t.Fatalf("expected one port binding, got %#v", spec.PortBindings)
	}
}

func TestDockerBuildContainerSpecUsesProvidedRuntimeEnv(t *testing.T) {
	root := t.TempDir()
	spec, err := docker.BuildContainerSpecWithEnv("start", &domain.Procedure{
		Image: "alpine:3.20",
		Env: map[string]string{
			"PROCEDURE_ONLY": "ignored",
		},
	}, root, nil, map[string]string{
		"DRUID_PORT_HTTP": "8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec.Env, []string{"DRUID_PORT_HTTP=8080"}) {
		t.Fatalf("env = %#v", spec.Env)
	}
}

func TestDockerRunCommandDefaultsMountSubPathToRoot(t *testing.T) {
	root := t.TempDir()
	spec, err := docker.BuildContainerSpec("start", &domain.Procedure{
		Image:   "alpine:3.20",
		Mounts:  []domain.Mount{{Path: "/server"}},
		Command: []string{"true"},
	}, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectedMounts := []mount.Mount{{
		Type:        mount.TypeBind,
		Source:      filepath.Join(root, "data"),
		Target:      "/server",
		BindOptions: &mount.BindOptions{CreateMountpoint: true},
	}}
	if !reflect.DeepEqual(spec.Mounts, expectedMounts) {
		t.Fatalf("unexpected mounts:\nexpected: %#v\nactual:   %#v", expectedMounts, spec.Mounts)
	}
}

func TestDockerContainerSpecUsesVolumeSubpath(t *testing.T) {
	spec, err := docker.BuildContainerSpec("start", &domain.Procedure{
		Image:   "alpine:3.20",
		Mounts:  []domain.Mount{{Path: "/server", SubPath: "public", ReadOnly: true}},
		Command: []string{"true"},
	}, "docker-volume://druid-scroll-data", nil)
	if err != nil {
		t.Fatal(err)
	}
	expectedMounts := []mount.Mount{{
		Type:          mount.TypeVolume,
		Source:        "druid-scroll-data",
		Target:        "/server",
		ReadOnly:      true,
		VolumeOptions: &mount.VolumeOptions{Subpath: "data/public"},
	}}
	if !reflect.DeepEqual(spec.Mounts, expectedMounts) {
		t.Fatalf("unexpected mounts:\nexpected: %#v\nactual:   %#v", expectedMounts, spec.Mounts)
	}
}

func TestDockerBuildContainerSpecSupportsTTY(t *testing.T) {
	root := t.TempDir()

	spec, err := docker.BuildContainerSpec("build.0", &domain.Procedure{
		Image:      domain.DefaultExecImage,
		Command:    []string{"bash", "-lc", "echo ok"},
		WorkingDir: "/work",
		TTY:        true,
	}, root, nil)
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
