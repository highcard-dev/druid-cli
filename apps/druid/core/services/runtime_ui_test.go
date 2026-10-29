package services

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	if values["DRUID_UI_SOURCE"] != "private/dist/app.wasm" || values["DRUID_UI_UPLOAD_URL"] == "" || values["DRUID_UI_DOCUMENT_UPLOAD_URL"] == "" {
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

func TestPublishUIPackageFailureKeepsPreviousPackage(t *testing.T) {
	store := newTestStateStore(t)
	previous := domain.RuntimeUIPackage{URL: "http://packages/previous/app.wasm", Path: "private/dist/app.wasm"}
	if err := store.CreateScroll(&domain.RuntimeScroll{
		ID: "ui-scroll", Root: "runtime://ui-scroll", ScrollName: "cached", ScrollYAML: cachedScrollYAML(""),
		UIPackages: domain.RuntimeUIPackages{domain.RuntimeUIPackageScopePrivate: previous},
	}); err != nil {
		t.Fatal(err)
	}
	backend := &fakeWorkerBackend{runCommand: func(ports.RuntimeCommand) (*int, error) {
		return nil, errors.New("document upload failed")
	}}
	supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
	if _, err := supervisor.PublishUIPackage("ui-scroll", "private", ""); err == nil {
		t.Fatal("expected upload failure")
	}
	current, err := store.GetScroll("ui-scroll")
	if err != nil {
		t.Fatal(err)
	}
	if current.UIPackages[domain.RuntimeUIPackageScopePrivate] != previous {
		t.Fatalf("failed publication changed active package: %#v", current.UIPackages)
	}
}

func TestUIPublishRequiresBothAssetsAndStopsOnUploadFailure(t *testing.T) {
	for _, test := range []struct {
		name        string
		document    bool
		fail        bool
		wantErr     bool
		wantUploads int
	}{
		{"missing document", false, false, true, 0},
		{"missing wasm", true, false, true, 0},
		{"empty wasm", true, false, true, 0},
		{"empty document", true, false, true, 0},
		{"document upload fails", true, true, true, 1},
		{"complete package", true, false, false, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "private"), 0755); err != nil {
				t.Fatal(err)
			}
			if test.name != "missing wasm" {
				data := []byte("wasm")
				if test.name == "empty wasm" {
					data = nil
				}
				if err := os.WriteFile(filepath.Join(dir, "private", "app.wasm"), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if test.document {
				data := []byte("root = Text(\"Test\")")
				if test.name == "empty document" {
					data = nil
				}
				if err := os.WriteFile(filepath.Join(dir, "private", "app.openui"), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$UPLOAD_LOG\"\n"
			if test.fail {
				script += "exit 7\n"
			}
			if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			args := uiPublishCommand().Procedures[0].Command[1:]
			// Stored Scrolls expand environment variables before a worker sees the
			// command. Request-scoped values must survive that round trip.
			args[1] = os.ExpandEnv(args[1])
			command := exec.Command("sh", args...)
			command.Dir = dir
			log := filepath.Join(dir, "uploads")
			command.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "UPLOAD_LOG="+log,
				"DRUID_UI_SOURCE=private/app.wasm", "DRUID_UI_DOCUMENT_SOURCE=private/app.openui", "DRUID_UI_UPLOAD_URL=http://uploads/app.wasm", "DRUID_UI_DOCUMENT_UPLOAD_URL=http://uploads/app.openui")
			output, err := command.CombinedOutput()
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, output = %s", err, output)
			}
			data, _ := os.ReadFile(log)
			if got := strings.Count(string(data), "--upload-file"); got != test.wantUploads {
				t.Fatalf("uploads = %d, want %d", got, test.wantUploads)
			}
			if test.wantUploads == 2 && !strings.Contains(string(data), "app.openui") {
				t.Fatal("missing document upload")
			}
		})
	}
}
