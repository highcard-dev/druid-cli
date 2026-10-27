package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/ports"
)

func TestInspectWorkerReadsInstalledReleaseNotRegistryReference(t *testing.T) {
	root := t.TempDir()
	want := "sha256:" + strings.Repeat("a", 64)
	raw := "{\n  \"digest\": \"" + want + "\"\n}"
	mustWrite(t, filepath.Join(root, "manifest.json"), raw)
	mustWrite(t, filepath.Join(root, "scroll.yaml"), "name: registry.local/owned/example\napp_version: '1'\ncommands: {}\n")
	result := runWorkerPull(ports.RuntimeWorkerAction{Mode: ports.RuntimeWorkerModeInspect, MountPath: root, Artifact: "unreachable.invalid/backup:latest"})
	if result.Error != "" || result.ArtifactDigest != want {
		t.Fatalf("inspection = %#v", result)
	}
	assertFile(t, filepath.Join(root, "manifest.json"), raw)
}

func TestInspectWorkerRejectsMissingOrInvalidInstalledDescriptor(t *testing.T) {
	for _, raw := range []string{"", "{}", `{"digest":"sha256:bad"}`} {
		t.Run(raw, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, "scroll.yaml"), "name: fixture\n")
			if raw != "" {
				mustWrite(t, filepath.Join(root, "manifest.json"), raw)
			}
			if result := inspectInstalledRelease(root); result.Error == "" {
				t.Fatal("invalid installed release was accepted")
			}
		})
	}
}

func TestPullWorkerReportsMaterializedRelease(t *testing.T) {
	for _, mode := range []ports.RuntimeWorkerMode{ports.RuntimeWorkerModeCreate, ports.RuntimeWorkerModeUpdate} {
		t.Run(string(mode), func(t *testing.T) {
			source, root := t.TempDir(), t.TempDir()
			want := "sha256:" + strings.Repeat("a", 64)
			yaml := "name: registry.local/release\napp_version: '2'\ncommands: {}\n"
			mustWrite(t, filepath.Join(source, "manifest.json"), `{"digest":"`+want+`"}`)
			mustWrite(t, filepath.Join(source, "scroll.yaml"), yaml)
			mustWrite(t, filepath.Join(root, "scroll.yaml"), "name: previous\n")
			result := runWorkerPull(ports.RuntimeWorkerAction{Mode: mode, MountPath: root, Artifact: source})
			if result.Error != "" || result.ArtifactDigest != want || result.ScrollYAML != yaml {
				t.Fatalf("materialized result = %#v", result)
			}
		})
	}
}

func TestPullWorkerAllowsLocalScrollWithoutReleaseDescriptor(t *testing.T) {
	source, root := t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(source, "scroll.yaml"), "name: local\n")
	result := runWorkerPull(ports.RuntimeWorkerAction{Mode: ports.RuntimeWorkerModeCreate, MountPath: root, Artifact: source})
	if result.Error != "" || result.ArtifactDigest != "" || result.ScrollYAML != "name: local\n" {
		t.Fatalf("local result = %#v", result)
	}
}

func TestPreserveSkippedUpdateDataRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	stage, err := os.MkdirTemp(root, ".stage-")
	if err != nil {
		t.Fatal(err)
	}
	if err := preserveSkippedUpdateData(root, stage, map[string]bool{"../../outside": true}); err == nil {
		t.Fatal("unsafe skip_update path accepted")
	}
}

func TestPreserveSkippedUpdateDataDoesNotFollowSymlinks(t *testing.T) {
	root, stage, outside := t.TempDir(), t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(outside, "save"), "outside runtime")
	if err := os.MkdirAll(filepath.Join(root, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "data", "link")); err != nil {
		t.Fatal(err)
	}
	if err := preserveSkippedUpdateData(root, stage, map[string]bool{"link/save": true}); err == nil {
		t.Fatal("followed a protected path through an external symlink")
	}
	if err := preserveSkippedUpdateData(root, stage, map[string]bool{"link": true}); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(stage, "data", "link")); err != nil || target != outside {
		t.Fatalf("protected symlink was dereferenced: %q (%v)", target, err)
	}
	assertFile(t, filepath.Join(outside, "save"), "outside runtime")
}
