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
		ID: "ui-scroll", Root: "runtime://ui-scroll", ScrollName: "ui-scroll", ScrollYAML: "name: ui-scroll\ndescription: UI package test\n",
	}); err != nil {
		t.Fatal(err)
	}
	var command ports.RuntimeCommand
	backend := &fakeWorkerBackend{runCommand: func(got ports.RuntimeCommand) (*int, error) {
		command = got
		return nil, nil
	}}
	supervisor := NewRuntimeSupervisor(store, coreservices.NewRuntimeScrollManager(store), backend)

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
	procedure := domain.ProcedureName(command.Name, 0, command.Command.Procedures[0])
	values := command.ProcedureEnv[procedure]
	if values["DRUID_UI_SOURCE"] != "private/dist/app.wasm" || !strings.HasPrefix(values["DRUID_UI_UPLOAD_URL"], "http://uploads/ui-scroll/private") {
		t.Fatalf("procedure env = %#v", values)
	}
	if strings.Contains(strings.Join(command.Command.Procedures[0].Command, " "), values["DRUID_UI_UPLOAD_URL"]) {
		t.Fatal("upload URL must not be persisted in command arguments")
	}
}
