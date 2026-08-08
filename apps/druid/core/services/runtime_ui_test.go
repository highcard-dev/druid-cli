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

func TestPublishUIPackageCreatesPendingRequest(t *testing.T) {
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
	request := updated.UIPackagePublishes[domain.RuntimeUIPackageScopePrivate]
	if request.Status != domain.UIPackagePublishPending || request.Path != "private/dist/app.wasm" || request.ID == "" {
		t.Fatalf("publish request = %#v", request)
	}
	if request.UpdatedAt.IsZero() || request.ExpiresAt.IsZero() {
		t.Fatal("updated_at should be set")
	}
}

func TestUIPackagePublishOnlyCompletesForClaimingWorkload(t *testing.T) {
	store := newTestStateStore(t)
	if err := store.CreateScroll(&domain.RuntimeScroll{ID: "ui-scroll", Root: "runtime://ui-scroll", ScrollName: "ui-scroll", ScrollYAML: "name: ui-scroll\n"}); err != nil {
		t.Fatal(err)
	}
	supervisor := NewRuntimeSupervisor(store, coreservices.NewRuntimeScrollManager(store), &fakeWorkerBackend{})
	created, err := supervisor.PublishUIPackage("ui-scroll", "private", "")
	if err != nil {
		t.Fatal(err)
	}
	request := created.UIPackagePublishes[domain.RuntimeUIPackageScopePrivate]
	claimed, err := supervisor.ClaimUIPackagePublish("ui-scroll", "pod-a")
	if err != nil || claimed == nil || claimed.ID != request.ID {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	digest := strings.Repeat("a", 64)
	if _, err := supervisor.PrepareUIPackagePublish("ui-scroll", request.ID, "pod-b", digest, 42); err == nil {
		t.Fatal("different workload must not prepare the claimed publish")
	}
	if _, err := supervisor.PrepareUIPackagePublish("ui-scroll", request.ID, "pod-a", digest, 42); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.CompleteUIPackagePublish("ui-scroll", request.ID, "pod-a", strings.Repeat("b", 64), 42); err == nil {
		t.Fatal("completion must match the prepared checksum")
	}
	completed, err := supervisor.CompleteUIPackagePublish("ui-scroll", request.ID, "pod-a", digest, 42)
	if err != nil {
		t.Fatal(err)
	}
	if packageInfo := completed.UIPackages[domain.RuntimeUIPackageScopePrivate]; packageInfo.SHA256 != digest || packageInfo.Path != "private/dist/app.wasm" {
		t.Fatalf("package = %#v", packageInfo)
	}
}
