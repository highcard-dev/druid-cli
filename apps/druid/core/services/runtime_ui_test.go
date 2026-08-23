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
	backend := &fakeWorkerBackend{runCommand: func(got ports.RuntimeCommand) (*int, error) {
		command = got
		return nil, nil
	}}
	supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)

	updated, err := supervisor.PublishUIPackage("ui-scroll", "private", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.UIPackages[domain.RuntimeUIPackageScopePrivate]; got.Path != "private/dist/app.wasm" || got.URL == "" {
		t.Fatalf("package = %#v", got)
	}
	if !strings.HasPrefix(command.Name, "ui_publish_private_") || command.Command == nil || len(command.Command.Procedures) != 1 {
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
	if values["DRUID_UI_SOURCE"] != "private/dist/app.wasm" || values["DRUID_UI_UPLOAD_URL"] == "" {
		t.Fatalf("procedure env = %#v", values)
	}
	script := command.Command.Procedures[0].Command[len(command.Command.Procedures[0].Command)-1]
	if !strings.Contains(script, "$(printenv DRUID_UI_SOURCE)") || !strings.Contains(script, "$(printenv DRUID_UI_UPLOAD_URL)") {
		t.Fatalf("publish command lost request-scoped environment references: %q", script)
	}
	if strings.Contains(script, "sha256") || strings.Contains(script, "content_md5") || strings.Contains(script, "prepare_url") {
		t.Fatal("publish command must upload directly without digest negotiation")
	}
}
