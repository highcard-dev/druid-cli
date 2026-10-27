package cli

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/highcard-dev/daemon/internal/utils"
)

func TestWorkerOwnerResolution(t *testing.T) {
	owner, err := resolveWorkerDataOwner("1234:2345")
	if err != nil || owner.uid != 1234 || owner.gid != 2345 {
		t.Fatalf("numeric owner: %+v %v", owner, err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	owner, err = resolveWorkerDataOwner(current.Username)
	if err != nil || owner.uid != os.Getuid() || owner.gid != os.Getgid() {
		t.Fatalf("named owner: %+v %v", owner, err)
	}
	for _, invalid := range []string{"-1:0", ":0", "1000:", "1:2:3", "4294967295:0", "0:4294967295"} {
		if _, err := resolveWorkerDataOwner(invalid); err == nil {
			t.Errorf("accepted invalid owner %q", invalid)
		}
	}
}

func TestWorkerOwnershipDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(t.TempDir(), "absent"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := applyWorkerDataOwner(root, &workerDataOwner{os.Getuid(), os.Getgid()}); err != nil {
		t.Fatal(err)
	}
	// A dangling target makes any accidental target dereference fail.
	target := filepath.Join(t.TempDir(), "copy")
	if err := utils.CopyPath(root, target, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(filepath.Join(target, "link")); err != nil {
		t.Fatal(err)
	}
}
