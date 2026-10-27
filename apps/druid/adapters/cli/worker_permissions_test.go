package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/highcard-dev/daemon/internal/utils"
)

func TestWorkerUpdatePreservesProtectedPermissions(t *testing.T) {
	root, candidate := t.TempDir(), t.TempDir()
	yaml := "name: example\nchunks:\n  - name: config\n    path: config\n    skip_update: true\n"
	mustWrite(t, filepath.Join(root, "scroll.yaml"), yaml)
	mustWrite(t, filepath.Join(candidate, "scroll.yaml"), yaml)
	mustWrite(t, filepath.Join(root, "data/config/settings.cfg"), "user settings")
	mustWrite(t, filepath.Join(candidate, "data/config/settings.cfg"), "new defaults")
	want := map[string]os.FileMode{"data/config": 0770, "data/config/settings.cfg": 0660}
	for path, mode := range want {
		if err := os.Chmod(filepath.Join(root, path), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := pullWorkerUpdate(root, candidate, nil, nil); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "data/config/settings.cfg"), "user settings")
	for path, mode := range want {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("protected %s mode=%#o, want %#o", path, info.Mode().Perm(), mode)
		}
	}
}

func TestProtectedDataCopyPreservesReadOnlyDirectoryPermissions(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(source, "config", "settings"), "user settings")
	if err := os.Chmod(filepath.Join(source, "config"), 0550); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(source, "config"), 0700)
	defer os.Chmod(filepath.Join(target, "config"), 0700)
	if err := utils.CopyPath(source, target, true); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(target, "config", "settings"), "user settings")
	info, err := os.Stat(filepath.Join(target, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0550 {
		t.Fatalf("directory mode=%#o, want 0550", info.Mode().Perm())
	}
}
