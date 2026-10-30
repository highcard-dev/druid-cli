package services

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

func uiScrollYAML() string {
	return cachedScrollYAML("") + "ui:\n  private:\n    wasm: private/dist/app.wasm\n    openui: private/templates/view.openui\n  public:\n    wasm: public/dist/app.wasm\n    openui: public/templates/view.openui\n"
}

func TestPublishWithBuildUsesFreshDependency(t *testing.T) {
	store := newTestStateStore(t)
	yaml := strings.Replace(uiScrollYAML(), "wasm: private/dist/app.wasm", "buildcommand: start\n    wasm: private/dist/app.wasm", 1)
	if err := store.CreateScroll(&domain.RuntimeScroll{ID: "build-ui", Root: "runtime://build-ui", ScrollName: "cached", ScrollYAML: yaml}); err != nil {
		t.Fatal(err)
	}
	var runs []string
	backend := &fakeWorkerBackend{runCommand: func(cmd ports.RuntimeCommand) (*int, error) {
		if cmd.Name == "start" {
			runs = append(runs, "build")
		} else {
			runs = append(runs, "upload")
			if !strings.HasPrefix(cmd.Name, "publish_with_build_") || !reflect.DeepEqual(cmd.Command.Needs, []string{"start"}) {
				t.Errorf("missing build dependency: %#v", cmd)
			}
		}
		return nil, nil
	}}
	supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
	for i := 0; i < 2; i++ {
		if _, err := supervisor.PublishUIPackage("build-ui", "private", true); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"build", "upload", "build", "upload"}
	if !reflect.DeepEqual(runs, want) {
		t.Fatalf("runs: %v, want %v", runs, want)
	}
}

func TestPublishRequiresDeclarationAndBuildOptIn(t *testing.T) {
	store := newTestStateStore(t)
	if err := store.CreateScroll(&domain.RuntimeScroll{ID: "ui-scroll", Root: "runtime://ui-scroll", ScrollName: "cached", ScrollYAML: uiScrollYAML()}); err != nil {
		t.Fatal(err)
	}
	supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), &fakeWorkerBackend{})
	if _, err := supervisor.PublishUIPackage("ui-scroll", "private", true); err == nil {
		t.Fatal("accepted build without a buildcommand")
	}
	if _, err := supervisor.PublishUIPackage("ui-scroll", "unknown", false); err == nil {
		t.Fatal("accepted unknown scope")
	}
}

func TestPublishUIPackageRunsOneShotCommandWithEphemeralURL(t *testing.T) {
	store := newTestStateStore(t)
	if err := store.CreateScroll(&domain.RuntimeScroll{
		ID: "ui-scroll", Root: "runtime://ui-scroll", ScrollName: "cached", ScrollYAML: uiScrollYAML(),
	}); err != nil {
		t.Fatal(err)
	}
	var command ports.RuntimeCommand
	backend := &fakeWorkerBackend{runCommand: func(got ports.RuntimeCommand) (*int, error) {
		command = got
		return nil, nil
	}}
	supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)

	updated, err := supervisor.PublishUIPackage("ui-scroll", "private", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.UIPackages[domain.RuntimeUIPackageScopePrivate]; got.Path != "private/dist/app.wasm" || got.URL == "" {
		t.Fatalf("package = %#v", got)
	}
	if !strings.HasPrefix(command.Name, "ui_publish_") || command.Command == nil || len(command.Command.Procedures) != 1 {
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
	if values["DRUID_UI_PRIVATE_DOCUMENT_SOURCE"] != "private/templates/view.openui" || updated.UIPackages[domain.RuntimeUIPackageScopePrivate].OpenUIURL == "" {
		t.Fatal("explicit document path or URL missing")
	}
	if values["DRUID_UI_PRIVATE_SOURCE"] != "private/dist/app.wasm" || values["DRUID_UI_PRIVATE_UPLOAD_URL"] == "" || values["DRUID_UI_PRIVATE_DOCUMENT_UPLOAD_URL"] == "" {
		t.Fatalf("procedure env = %#v", values)
	}
	script := command.Command.Procedures[0].Command[len(command.Command.Procedures[0].Command)-1]
	if !strings.Contains(script, "$(printenv DRUID_UI_PRIVATE_SOURCE)") || !strings.Contains(script, "$(printenv DRUID_UI_PRIVATE_UPLOAD_URL)") {
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
		ID: "ui-scroll", Root: "runtime://ui-scroll", ScrollName: "cached", ScrollYAML: uiScrollYAML(),
		UIPackages: domain.RuntimeUIPackages{domain.RuntimeUIPackageScopePrivate: previous},
	}); err != nil {
		t.Fatal(err)
	}
	backend := &fakeWorkerBackend{runCommand: func(ports.RuntimeCommand) (*int, error) {
		return nil, errors.New("document upload failed")
	}}
	supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
	if _, err := supervisor.PublishUIPackage("ui-scroll", "private", false); err == nil {
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
			args := uiPublishCommand([]domain.RuntimeUIPackageScope{domain.RuntimeUIPackageScopePrivate}).Procedures[0].Command[1:]
			// Stored Scrolls expand environment variables before a worker sees the
			// command. Request-scoped values must survive that round trip.
			args[1] = os.ExpandEnv(args[1])
			command := exec.Command("sh", args...)
			command.Dir = dir
			log := filepath.Join(dir, "uploads")
			command.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "UPLOAD_LOG="+log,
				"DRUID_UI_PRIVATE_SOURCE=private/app.wasm", "DRUID_UI_PRIVATE_DOCUMENT_SOURCE=private/app.openui", "DRUID_UI_PRIVATE_UPLOAD_URL=http://uploads/app.wasm", "DRUID_UI_PRIVATE_DOCUMENT_UPLOAD_URL=http://uploads/app.openui")
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

func TestDeclaredUIPackagesShareOneCommandAndRetainReferencesOnFailure(t *testing.T) {
	for _, scopes := range [][]domain.RuntimeUIPackageScope{{domain.RuntimeUIPackageScopePrivate}, {domain.RuntimeUIPackageScopePublic}, {domain.RuntimeUIPackageScopePrivate, domain.RuntimeUIPackageScopePublic}} {
		for _, fail := range []bool{false, true} {
			store := newTestStateStore(t)
			previous := domain.RuntimeUIPackages{
				domain.RuntimeUIPackageScopePrivate: {URL: "http://old/private.wasm"},
				domain.RuntimeUIPackageScopePublic:  {URL: "http://old/public.wasm"},
			}
			runtime := &domain.RuntimeScroll{ID: "batch", Root: "runtime://batch", ScrollName: "cached", ScrollYAML: cachedScrollYAML(""), UIPackages: previous}
			if err := store.CreateScroll(runtime); err != nil {
				t.Fatal(err)
			}
			paths := map[domain.RuntimeUIPackageScope]*domain.UIPackageDeclaration{}
			for _, scope := range scopes {
				paths[scope] = &domain.UIPackageDeclaration{WASM: string(scope) + "/dist/app.wasm", OpenUI: string(scope) + "/view.openui"}
			}
			runs := 0
			backend := &fakeWorkerBackend{runCommand: func(command ports.RuntimeCommand) (*int, error) {
				runs++
				if len(command.Command.Procedures) != 1 {
					t.Fatal("upload used multiple containers")
				}
				for _, scope := range scopes {
					procedure := domain.ProcedureName(command.Name, 0, command.Command.Procedures[0])
					prefix := "DRUID_UI_" + strings.ToUpper(string(scope)) + "_"
					if command.ProcedureEnv[procedure][prefix+"SOURCE"] != paths[scope].WASM {
						t.Fatal("scope source missing")
					}
				}
				if fail {
					return nil, errors.New("last upload failed")
				}
				return nil, nil
			}}
			supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
			_, err := supervisor.publishUIPackages("batch", paths, false)
			if (err != nil) != fail || runs != 1 {
				t.Fatalf("runs=%d err=%v fail=%v", runs, err, fail)
			}
			current, err := store.GetScroll("batch")
			if err != nil {
				t.Fatal(err)
			}
			for scope := range paths {
				oldURL := "http://old/" + string(scope) + ".wasm"
				if fail && current.UIPackages[scope].URL != oldURL {
					t.Fatal("failed upload changed a reference")
				}
				if !fail && current.UIPackages[scope].URL == oldURL {
					t.Fatal("successful upload did not update reference")
				}
			}
		}
	}
}

func TestCombinedUploadChecksPublicPairBeforeAnyUpload(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "private"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app.wasm", "app.openui"} {
		if err := os.WriteFile(filepath.Join(dir, "private", name), []byte("fixture"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(dir, "uploads")
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte("#!/bin/sh\necho upload >> \"$UPLOAD_LOG\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	script := uiPublishCommand([]domain.RuntimeUIPackageScope{domain.RuntimeUIPackageScopePrivate, domain.RuntimeUIPackageScopePublic}).Procedures[0].Command[2]
	cmd := exec.Command("sh", "-ec", os.ExpandEnv(script))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "UPLOAD_LOG="+log)
	for _, scope := range []string{"PRIVATE", "PUBLIC"} {
		prefix := "DRUID_UI_" + scope + "_"
		cmd.Env = append(cmd.Env, prefix+"SOURCE="+strings.ToLower(scope)+"/app.wasm", prefix+"DOCUMENT_SOURCE="+strings.ToLower(scope)+"/app.openui", prefix+"UPLOAD_URL=http://upload", prefix+"DOCUMENT_UPLOAD_URL=http://upload/document")
	}
	if err := cmd.Run(); err == nil {
		t.Fatal("missing public files accepted")
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("uploaded private files before validating public pair")
	}
}
