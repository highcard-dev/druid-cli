package services

import (
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
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
	for _, path := range []string{"data/private/dist/app.wasm", "data/public/dist/app.wasm", "../private/dist/app.wasm", "private/dist/app.js", "public/dist/app.wasm"} {
		t.Run(path, func(t *testing.T) {
			if _, _, err := normalizeUIPackageRequest("private", path); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPublishUIPackageRunsOneShotCommandWithEphemeralURL(t *testing.T) {
	store := newTestStateStore(t)
	if err := store.CreateScroll(&domain.RuntimeScroll{
		ID: "ui-scroll", Root: "runtime://ui-scroll", ScrollName: "cached", ScrollYAML: cachedScrollYAML(""),
	}); err != nil {
		t.Fatal(err)
	}
	var command ports.RuntimeCommand
	var supervisor *RuntimeSupervisor
	backend := &fakeWorkerBackend{runCommand: func(got ports.RuntimeCommand) (*int, error) {
		command = got
		procedure := domain.ProcedureName(got.Name, 0, got.Command.Procedures[0])
		values := got.ProcedureEnv[procedure]
		if _, err := supervisor.PrepareUIPackageUpload("ui-scroll", "private", values["DRUID_UI_REQUEST_ID"], strings.Repeat("a", 64), "AAAAAAAAAAAAAAAAAAAAAA=="); err != nil {
			return nil, err
		}
		return nil, nil
	}}
	supervisor = NewRuntimeSupervisor(store, coreservices.NewRuntimeScrollManager(store), backend)
	supervisor.SetDevWorkerConfig("http://daemon", "", "")

	updated, err := supervisor.PublishUIPackage("ui-scroll", "private", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.UIPackages[domain.RuntimeUIPackageScopePrivate]; got.Path != "private/dist/app.wasm" || got.SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("package = %#v", got)
	}
	if command.Name != "ui_publish_private" || command.Command == nil || len(command.Command.Procedures) != 1 {
		t.Fatalf("command = %#v", command)
	}
	if got := command.Command.Procedures[0].Image; got != uiPublishImage {
		t.Fatalf("image = %q, want %q", got, uiPublishImage)
	}
	mounts := command.Command.Procedures[0].Mounts
	if len(mounts) != 1 || mounts[0].Path != "/app/resources/deployment" || mounts[0].SubPath != "." || !mounts[0].ReadOnly {
		t.Fatalf("UI publish mount = %#v, want read-only PVC root", mounts)
	}
	procedure := domain.ProcedureName(command.Name, 0, command.Command.Procedures[0])
	values := command.ProcedureEnv[procedure]
	if values["DRUID_UI_SOURCE"] != "private/dist/app.wasm" || values["DRUID_UI_REQUEST_ID"] == "" || values["DRUID_UI_PREPARE_URL"] != "http://daemon/api/v1/scrolls/ui-scroll/ui/packages/private/prepare" {
		t.Fatalf("procedure env = %#v", values)
	}
	if strings.Contains(strings.Join(command.Command.Procedures[0].Command, " "), "http://uploads/") {
		t.Fatal("upload URL must not be persisted in command arguments")
	}
	script := strings.Join(command.Command.Procedures[0].Command, " ")
	if !strings.Contains(script, "verify_url=") || !strings.Contains(script, "uploaded UI package SHA-256 verification failed") {
		t.Fatal("publish command must verify the uploaded public object before completion")
	}
}
