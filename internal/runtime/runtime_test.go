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
	if len(spec.PortBindings) != 0 {
		t.Fatalf("expected no implicit port bindings, got %#v", spec.PortBindings)
	}
}

func TestDockerBuildContainerSpecPublishesOnlyAssignedRouting(t *testing.T) {
	root := t.TempDir()
	spec, err := docker.BuildContainerSpecWithRouting("start", &domain.Procedure{
		Image:         "alpine:3.20",
		ExpectedPorts: []domain.ExpectedPort{{Name: "http"}},
	}, root, []domain.Port{{Name: "http", Port: 8080, Protocol: "http"}}, []domain.RuntimeRouteAssignment{{
		PortName:   "http",
		ExternalIP: "127.0.0.1",
		PublicPort: 18080,
		Protocol:   "https",
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bindings := spec.PortBindings["8080/tcp"]
	if len(bindings) != 1 || bindings[0].HostIP != "127.0.0.1" || bindings[0].HostPort != "18080" {
		t.Fatalf("bindings = %#v", spec.PortBindings)
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
