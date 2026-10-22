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
			if raw != "" {
				mustWrite(t, filepath.Join(root, "manifest.json"), raw)
			}
			if result := inspectInstalledRelease(root); result.Error == "" {
				t.Fatal("invalid installed release was accepted")
			}
		})
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
