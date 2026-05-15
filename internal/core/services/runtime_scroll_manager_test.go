package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
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
	store := newMemoryRuntimeStore(t.TempDir())
	manager := NewRuntimeScrollManager(store)

	if _, err := manager.Create("artifact", "", t.TempDir(), []byte(testScrollYAML)); err != nil {
		t.Fatal(err)
	}
	_, err := manager.Create("artifact", "", t.TempDir(), []byte(testScrollYAML))
	if !errors.Is(err, domain.ErrRuntimeScrollAlreadyExists) {
		t.Fatalf("error = %v, want domain.ErrRuntimeScrollAlreadyExists", err)
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

type memoryRuntimeStore struct {
	stateDir string
	scrolls  map[string]*domain.RuntimeScroll
}

func newMemoryRuntimeStore(stateDir string) *memoryRuntimeStore {
	return &memoryRuntimeStore{stateDir: stateDir, scrolls: map[string]*domain.RuntimeScroll{}}
}

func (s *memoryRuntimeStore) StateDir() string { return s.stateDir }

func (s *memoryRuntimeStore) Root(id string) string {
	return filepath.Join(s.stateDir, "scrolls", id)
}

func (s *memoryRuntimeStore) CreateScroll(scroll *domain.RuntimeScroll) error {
	s.scrolls[scroll.ID] = scroll
	return nil
}

func (s *memoryRuntimeStore) ListScrolls() ([]*domain.RuntimeScroll, error) {
	scrolls := make([]*domain.RuntimeScroll, 0, len(s.scrolls))
	for _, scroll := range s.scrolls {
		scrolls = append(scrolls, scroll)
	}
	return scrolls, nil
}

func (s *memoryRuntimeStore) GetScroll(id string) (*domain.RuntimeScroll, error) {
	scroll, ok := s.scrolls[id]
	if !ok {
		return nil, domain.ErrRuntimeScrollNotFound
	}
	return scroll, nil
}

func (s *memoryRuntimeStore) UpdateScroll(scroll *domain.RuntimeScroll) error {
	if _, ok := s.scrolls[scroll.ID]; !ok {
		return domain.ErrRuntimeScrollNotFound
	}
	s.scrolls[scroll.ID] = scroll
	return nil
}

func (s *memoryRuntimeStore) DeleteScroll(id string) error {
	if _, ok := s.scrolls[id]; !ok {
		return domain.ErrRuntimeScrollNotFound
	}
	delete(s.scrolls, id)
	return nil
}
