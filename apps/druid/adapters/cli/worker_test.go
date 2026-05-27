package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"oras.land/oras-go/v2/registry/remote"
)

func TestWorkerPullCommandUsesRuntimeIDOnly(t *testing.T) {
	if flag := WorkerPullCommand.Flags().Lookup("runtime-id"); flag == nil {
		t.Fatal("worker pull should expose --runtime-id")
	}
	if flag := WorkerPullCommand.Flags().Lookup("action-id"); flag != nil {
		t.Fatal("worker pull should not expose --action-id")
	}
}

func TestWorkerPullCommandRequiresRuntimeID(t *testing.T) {
	flag := WorkerPullCommand.Flags().Lookup("runtime-id")
	if flag == nil {
		t.Fatal("worker pull should expose --runtime-id")
	}
	if got := flag.Annotations[cobra.BashCompOneRequiredFlag]; len(got) != 1 || got[0] != "true" {
		t.Fatalf("runtime-id required annotation = %#v, want true", got)
	}
}

func TestWorkerUpdateMergePreservesSkipUpdateAndExtraFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	mustWrite(t, filepath.Join(src, "scroll.yaml"), "name: next\n")
	mustWrite(t, filepath.Join(src, "data", "keep", "state.txt"), "new")
	mustWrite(t, filepath.Join(src, "data", "overwrite.txt"), "new")
	mustWrite(t, filepath.Join(dst, "scroll.yaml"), "name: old\n")
	mustWrite(t, filepath.Join(dst, "data", "keep", "state.txt"), "old")
	mustWrite(t, filepath.Join(dst, "data", "overwrite.txt"), "old")
	mustWrite(t, filepath.Join(dst, "data", "extra.txt"), "extra")

	if err := mergePulledRoot(src, dst, map[string]bool{"keep": true}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dst, "scroll.yaml"), "name: next\n")
	assertFile(t, filepath.Join(dst, "data", "keep", "state.txt"), "old")
	assertFile(t, filepath.Join(dst, "data", "overwrite.txt"), "new")
	assertFile(t, filepath.Join(dst, "data", "extra.txt"), "extra")
}

func TestWorkerRestoreStagesBeforeReplacingRoot(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "scroll.yaml"), "name: old\n")
	mustWrite(t, filepath.Join(root, "data", "logs", "latest.log"), "old")
	mustWrite(t, filepath.Join(root, "data", "old-only.txt"), "old")

	oci := fakeRestoreOCI{t: t}
	if err := pullWorkerRestore(root, "registry.local/backup:1", oci); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(root, "scroll.yaml"), "name: restored\n")
	assertFile(t, filepath.Join(root, "data", "logs", "latest.log"), "restored")
	if _, err := os.Stat(filepath.Join(root, "data", "old-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("old root contents should be removed before restore, stat err = %v", err)
	}
}

func TestWorkerCollectSkipUpdatePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	mustWrite(t, filepath.Join(root, "scroll.yaml"), `name: skip-test
desc: test
version: 0.1.0
app_version: "1"
serve: start
chunks:
  - name: saves
    path: saves
    skip_update: true
  - name: parent
    path: server
    chunks:
      - name: cache
        path: cache
        skip_update: true
commands:
  start:
    procedures:
      - image: alpine:3.20
        command: ["true"]
`)
	result := runWorkerPullForSkipPathTest(t, root)
	if !result["saves"] || !result["server/cache"] {
		t.Fatalf("skip paths = %#v", result)
	}
}

func runWorkerPullForSkipPathTest(t *testing.T, root string) map[string]bool {
	t.Helper()
	scrollYAML, err := os.ReadFile(filepath.Join(root, "scroll.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	scroll, err := domain.NewScrollFromBytes(root, scrollYAML)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]bool{}
	collectSkipUpdatePaths(result, "", scroll.Chunks)
	return result
}

func mustWrite(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}

type fakeRestoreOCI struct {
	t *testing.T
}

func (fakeRestoreOCI) GetRepo(string) (*remote.Repository, error) {
	return nil, nil
}

func (fakeRestoreOCI) Push(string, string, string, map[string]string, bool, *domain.File) (v1.Descriptor, error) {
	return v1.Descriptor{}, nil
}

func (f fakeRestoreOCI) Pull(dir string, artifact string) error {
	return f.PullSelective(dir, artifact, true, nil)
}

func (f fakeRestoreOCI) PullSelective(dir string, artifact string, includeData bool, progress *domain.SnapshotProgress) error {
	mustWrite(f.t, filepath.Join(dir, "scroll.yaml"), "name: restored\n")
	mustWrite(f.t, filepath.Join(dir, "data", "logs", "latest.log"), "restored")
	return nil
}

func (fakeRestoreOCI) FetchFile(string, string) ([]byte, error) {
	return nil, os.ErrNotExist
}

func (fakeRestoreOCI) ValidateCredentials(string, string, string) error {
	return nil
}

func (fakeRestoreOCI) ResolveDigest(string) (string, error) {
	return "sha256:restored", nil
}

func (fakeRestoreOCI) CanUpdateTag(v1.Descriptor, string, string) (bool, error) {
	return false, nil
}
