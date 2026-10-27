package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkerUpdatePreservesGlobProtectedData(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for _, declaration := range []string{"installed", "candidate"} {
			t.Run(declaration+map[bool]string{false: "/flat", true: "/nested"}[nested], func(t *testing.T) {
				root, candidate := t.TempDir(), t.TempDir()
				chunks := "chunks:\n  - name: config\n    path: '*.cfg'\n    skip_update: true\n"
				directory := ""
				if nested {
					directory = "servers/one"
					chunks = "chunks:\n  - name: servers\n    path: 'servers/*'\n    chunks:\n      - name: config\n        path: '*.cfg'\n        skip_update: true\n"
				}
				installedYAML, candidateYAML := "name: example\n", "name: example\n"
				if declaration == "installed" {
					installedYAML += chunks
				} else {
					candidateYAML += chunks
				}
				mustWrite(t, filepath.Join(root, "scroll.yaml"), installedYAML)
				mustWrite(t, filepath.Join(candidate, "scroll.yaml"), candidateYAML)
				mustWrite(t, filepath.Join(root, "data", directory, "edited.cfg"), "user edits")
				mustWrite(t, filepath.Join(root, "data", directory, "removed.cfg"), "user save")
				mustWrite(t, filepath.Join(root, "data", directory, "obsolete.txt"), "old release")
				mustWrite(t, filepath.Join(candidate, "data", directory, "edited.cfg"), "new default")
				mustWrite(t, filepath.Join(candidate, "data", directory, "absent.cfg"), "must not appear")
				mustWrite(t, filepath.Join(candidate, "data", directory, "release.txt"), "new release")
				if err := pullWorkerUpdate(root, candidate, nil, nil); err != nil {
					t.Fatal(err)
				}
				assertFile(t, filepath.Join(root, "data", directory, "edited.cfg"), "user edits")
				assertFile(t, filepath.Join(root, "data", directory, "removed.cfg"), "user save")
				assertFile(t, filepath.Join(root, "data", directory, "release.txt"), "new release")
				for _, name := range []string{"absent.cfg", "obsolete.txt"} {
					if _, err := os.Stat(filepath.Join(root, "data", directory, name)); !os.IsNotExist(err) {
						t.Fatalf("%s should be absent: %v", name, err)
					}
				}
				assertFile(t, filepath.Join(root, "scroll.yaml"), candidateYAML)
			})
		}
	}
}
