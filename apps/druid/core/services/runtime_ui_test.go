package services

import (
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

func TestNormalizeUIPackageRequestDefaults(t *testing.T) {
	tests := []struct {
		scope string
		want  string
	}{
		{"private", "private/dist/app.wasm"},
		{"public", "public/dist/app.wasm"},
	}
	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			_, got, err := normalizeUIPackageRequest(tt.scope, "")
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeUIPackageRequestRejectsInvalidPaths(t *testing.T) {
	tests := []string{
		"data/private/dist/app.wasm",
		"data/public/dist/app.wasm",
		"../private/dist/app.wasm",
		"private/dist/app.js",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if _, _, err := normalizeUIPackageRequest("private", path); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPublishUIPackagePersistsMetadata(t *testing.T) {
	store := newTestStateStore(t)
	if err := store.CreateScroll(&domain.RuntimeScroll{
		ID:         "ui-scroll",
		Root:       "runtime://ui-scroll",
		ScrollName: "ui-scroll",
		ScrollYAML: "name: ui-scroll\n",
		Status:     domain.RuntimeScrollStatusCreated,
	}); err != nil {
		t.Fatal(err)
	}
	supervisor := NewRuntimeSupervisor(store, coreservices.NewRuntimeScrollManager(store), &fakeWorkerBackend{})

	updated, err := supervisor.PublishUIPackage("ui-scroll", "private", "")
	if err != nil {
		t.Fatal(err)
	}
	pkg := updated.UIPackages[domain.RuntimeUIPackageScopePrivate]
	if !strings.Contains(pkg.URL, "/ui-scroll/private/") {
		t.Fatalf("url = %q, want private package URL", pkg.URL)
	}
	if pkg.Path != "private/dist/app.wasm" {
		t.Fatalf("path = %q", pkg.Path)
	}
	if pkg.SHA256 != "sha256" {
		t.Fatalf("sha = %q", pkg.SHA256)
	}
	if pkg.UpdatedAt.IsZero() {
		t.Fatal("updated_at should be set")
	}
}
