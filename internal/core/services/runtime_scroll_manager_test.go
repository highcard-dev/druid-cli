package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const testScrollYAML = `name: ghcr.io/druid-examples/static-web:1.0
desc: Static web
version: 0.1.0
app_version: "1.0"
serve: start
commands:
  start:
    procedures:
      - image: alpine:3.20
        command: ["true"]
`

func TestRuntimeScrollID(t *testing.T) {
	tests := []struct {
		name       string
		scrollName string
		want       string
	}{
		{name: "local dev", scrollName: "ignored", want: "local-dev"},
		{name: "", scrollName: "ghcr.io/druid-examples/static-web:1.0", want: "static-web"},
		{name: "", scrollName: "ghcr.io/druid-examples/static-web@sha256:abc", want: "static-web"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := RuntimeScrollID(tt.name, tt.scrollName)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("id = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRuntimeScrollManagerCreateFailsDuplicateID(t *testing.T) {
	store := NewRuntimeStateStore(t.TempDir())
	manager := NewRuntimeScrollManager(store)

	if _, err := manager.Create("artifact", "", t.TempDir(), []byte(testScrollYAML)); err != nil {
		t.Fatal(err)
	}
	_, err := manager.Create("artifact", "", t.TempDir(), []byte(testScrollYAML))
	if !errors.Is(err, ErrScrollAlreadyExists) {
		t.Fatalf("error = %v, want ErrScrollAlreadyExists", err)
	}
}

func TestRuntimeStateStoreUsesSingleRuntimeRoot(t *testing.T) {
	store := NewRuntimeStateStore(t.TempDir())
	if got, want := store.Root("scroll-a"), filepath.Join(store.StateDir(), "scrolls", "scroll-a"); got != want {
		t.Fatalf("Root = %s, want %s", got, want)
	}
}

func TestMaterializeScrollArtifactKeepsScrollYamlNextToData(t *testing.T) {
	artifact := t.TempDir()
	if err := os.WriteFile(filepath.Join(artifact, "scroll.yaml"), []byte(testScrollYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(artifact, "data", "private"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifact, "data", "private", "state.txt"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()

	if err := MaterializeScrollArtifact(artifact, root, nil, true); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "scroll.yaml")); err != nil {
		t.Fatalf("scroll.yaml not materialized next to data: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "data", "private", "state.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok" {
		t.Fatalf("state = %q, want ok", got)
	}
}
