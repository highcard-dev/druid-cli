package docker

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/docker/docker/api/types/mount"
)

func TestConfigWithDefaultsUsesDockerVolumeStorage(t *testing.T) {
	config := Config{}.WithDefaults()
	if config.Storage != StorageVolume {
		t.Fatalf("storage = %s, want volume", config.Storage)
	}
	if config.VolumePrefix != "druid" {
		t.Fatalf("volume prefix = %s, want druid", config.VolumePrefix)
	}
}

func TestRuntimeRootRefUsesVolumeByDefault(t *testing.T) {
	root, err := (Config{VolumePrefix: "lab"}).RuntimeRootRef("scroll a")
	if err != nil {
		t.Fatal(err)
	}
	if root != "docker-volume://lab-scroll-a-data" {
		t.Fatalf("root = %s", root)
	}
}

func TestRuntimeRootRefUsesBindRoot(t *testing.T) {
	bindRoot := filepath.Join(t.TempDir(), "roots")
	root, err := (Config{Storage: StorageBind, BindRoot: bindRoot}).RuntimeRootRef("scroll-a")
	if err != nil {
		t.Fatal(err)
	}
	if root != "docker-bind://"+filepath.Join(bindRoot, "scroll-a") {
		t.Fatalf("root = %s", root)
	}
}

func TestParseRootRefSupportsVolumeBindAndLocalBindPath(t *testing.T) {
	cases := map[string]RootRef{
		"docker-volume://druid-scroll-data": {Kind: StorageVolume, Source: "druid-scroll-data"},
		"docker-bind:///tmp/druid/scroll":   {Kind: StorageBind, Source: "/tmp/druid/scroll"},
		"/tmp/druid/local":                  {Kind: StorageBind, Source: "/tmp/druid/local"},
	}
	for input, want := range cases {
		got, err := ParseRootRef(input)
		if err != nil {
			t.Fatalf("ParseRootRef(%q): %v", input, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ParseRootRef(%q) = %#v, want %#v", input, got, want)
		}
	}
}

func TestDockerMountUsesVolumeSubpath(t *testing.T) {
	got, err := DockerMount("docker-volume://druid-scroll-data", "/site", true, "data/site")
	if err != nil {
		t.Fatal(err)
	}
	want := mount.Mount{
		Type:          mount.TypeVolume,
		Source:        "druid-scroll-data",
		Target:        "/site",
		ReadOnly:      true,
		VolumeOptions: &mount.VolumeOptions{Subpath: "data/site"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mount = %#v, want %#v", got, want)
	}
}

func TestDockerMountUsesBindSubpath(t *testing.T) {
	got, err := DockerMount("docker-bind:///tmp/druid/scroll", "/site", false, "data/site")
	if err != nil {
		t.Fatal(err)
	}
	want := mount.Mount{
		Type:        mount.TypeBind,
		Source:      "/tmp/druid/scroll/data/site",
		Target:      "/site",
		BindOptions: &mount.BindOptions{CreateMountpoint: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mount = %#v, want %#v", got, want)
	}
}

func TestDockerMountDotSubpathMountsRuntimeRoot(t *testing.T) {
	got, err := DockerMount("docker-volume://druid-scroll-data", "/runtime", false, procedureDataSubPath("."))
	if err != nil {
		t.Fatal(err)
	}
	if got.VolumeOptions != nil {
		t.Fatalf("dot subpath should mount the volume root, got %#v", got.VolumeOptions)
	}
}

func TestCleanContainerRelativePathRejectsTraversal(t *testing.T) {
	if _, err := CleanContainerRelativePath("../escape"); err == nil {
		t.Fatal("expected traversal error")
	}
	if got, err := CleanContainerRelativePath("/data/file.txt"); err != nil || got != "data/file.txt" {
		t.Fatalf("cleaned = %s err=%v", got, err)
	}
}
