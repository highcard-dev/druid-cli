package services

import (
	"errors"
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

	if _, err := manager.Create("artifact", "", t.TempDir(), filepath.Join(t.TempDir(), "data"), []byte(testScrollYAML)); err != nil {
		t.Fatal(err)
	}
	_, err := manager.Create("artifact", "", t.TempDir(), filepath.Join(t.TempDir(), "data"), []byte(testScrollYAML))
	if !errors.Is(err, ErrScrollAlreadyExists) {
		t.Fatalf("error = %v, want ErrScrollAlreadyExists", err)
	}
}
