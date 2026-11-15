package domain

import (
	"strings"
	"testing"

	semver "github.com/Masterminds/semver/v3"
)

func TestProcedureDefaultsToContainer(t *testing.T) {
	procedure := &Procedure{
		Image:   "alpine:3.20",
		Command: []string{"echo", "hello"},
	}

	if procedure.Kind() != ProcedureTypeContainer {
		t.Fatalf("Kind() = %s, want %s", procedure.Kind(), ProcedureTypeContainer)
	}

	scroll := testScroll(t, procedure)
	if err := scroll.Validate(false); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestPrivateUIPackagePathValidation(t *testing.T) {
	valid := testScroll(t, &Procedure{Image: "alpine:3.20"})
	valid.UI = &UIDeclaration{Private: &UIPackageDeclaration{WASM: "private/dist/app.wasm", OpenUI: "private/view.openui"}}
	if err := valid.Validate(false); err != nil {
		t.Fatalf("Validate() valid UI error = %v", err)
	}

	for _, invalidPath := range []string{"", "../private/app.wasm", "public/app.wasm", "private/app.js"} {
		scroll := testScroll(t, &Procedure{Image: "alpine:3.20"})
		scroll.UI = &UIDeclaration{Private: &UIPackageDeclaration{WASM: invalidPath, OpenUI: "private/view.openui"}}
		if err := scroll.Validate(false); err == nil {
			t.Fatalf("Validate() path %q error = nil", invalidPath)
		}
	}
}

func TestUIRequiresDocumentAndExistingBuildCommand(t *testing.T) {
	scroll := testScroll(t, &Procedure{Image: "alpine:3.20"})
	declaration := &UIPackageDeclaration{WASM: "private/artifact.wasm", OpenUI: "private/templates/view.openui"}
	scroll.UI = &UIDeclaration{Private: declaration}
	if err := scroll.Validate(false); err != nil {
		t.Fatal(err)
	}
	declaration.BuildCommand = "missing"
	if err := scroll.Validate(false); err == nil {
		t.Fatal("accepted missing buildcommand")
	}
	declaration.BuildCommand = ""
	for _, invalid := range []string{"", "public/view.openui", "../view.openui", "private/../view.openui", "private/view.json"} {
		declaration.OpenUI = invalid
		if err := scroll.Validate(false); err == nil {
			t.Fatalf("accepted document path %q", invalid)
		}
	}
}

func TestSignalProcedureValidation(t *testing.T) {
	scroll := testScroll(t, &Procedure{
		Type:   ProcedureTypeSignal,
		Target: "start",
		Signal: "SIGTERM",
	})

	if err := scroll.Validate(false); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestPublicUIAndCombinedDeclarations(t *testing.T) {
	for _, private := range []bool{false, true} {
		scroll := testScroll(t, &Procedure{Image: "alpine:3.20"})
		scroll.UI = &UIDeclaration{Public: &UIPackageDeclaration{WASM: "public/custom/app.wasm", OpenUI: "public/view.openui"}}
		if private {
			scroll.UI.Private = &UIPackageDeclaration{WASM: "private/dist/app.wasm", OpenUI: "private/view.openui"}
		}
		if err := scroll.Validate(false); err != nil {
			t.Fatal(err)
		}
		for _, invalid := range []string{"", "private/app.wasm", "../public/app.wasm", "public/app.js"} {
			scroll.UI.Public.WASM = invalid
			if err := scroll.Validate(false); err == nil {
				t.Fatalf("accepted public path %q", invalid)
			}
		}
	}
}

func TestUnsupportedProcedureFieldsRejected(t *testing.T) {
	tests := []struct {
		name      string
		procedure *Procedure
		want      string
	}{
		{
			name:      "mode",
			procedure: &Procedure{Mode: "scroll-switch"},
			want:      "field mode is unsupported",
		},
		{
			name:      "wait",
			procedure: &Procedure{Image: "alpine:3.20", Wait: false},
			want:      "field wait is unsupported",
		},
		{
			name:      "data",
			procedure: &Procedure{Image: "alpine:3.20", Data: "start"},
			want:      "field data is unsupported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scroll := testScroll(t, tt.procedure)
			err := scroll.Validate(false)
			if err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %q, want containing %q", err.Error(), tt.want)
			}
		})
	}
}

func TestScrollValidateAllowsMissingServe(t *testing.T) {
	scroll := testScroll(t, &Procedure{
		Image:   "alpine:3.20",
		Command: []string{"true"},
	})
	scroll.Serve = ""

	if err := scroll.Validate(false); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestScrollValidateRejectsUnknownServeCommand(t *testing.T) {
	scroll := testScroll(t, &Procedure{
		Image:   "alpine:3.20",
		Command: []string{"true"},
	})
	scroll.Serve = "missing"

	err := scroll.Validate(false)
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "serve command missing is not defined") {
		t.Fatalf("Validate() error = %q, want missing serve command", err.Error())
	}
}

func TestScrollValidateAllowsDynamicTransportPorts(t *testing.T) {
	scroll := testScroll(t, &Procedure{
		Image:   "alpine:3.20",
		Command: []string{"true"},
	})
	scroll.Ports = []Port{
		{Name: "tcp", Protocol: "tcp"},
		{Name: "udp", Protocol: "udp"},
		{Name: "web", Protocol: "http", Port: 8080},
	}

	if err := scroll.Validate(false); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestScrollValidateRejectsDynamicHTTPPorts(t *testing.T) {
	for _, protocol := range []string{"http", "https"} {
		t.Run(protocol, func(t *testing.T) {
			scroll := testScroll(t, &Procedure{
				Image:   "alpine:3.20",
				Command: []string{"true"},
			})
			scroll.Ports = []Port{{Name: "web", Protocol: protocol}}

			err := scroll.Validate(false)
			if err == nil || !strings.Contains(err.Error(), "requires a fixed port") {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func testScroll(t *testing.T, procedure *Procedure) *Scroll {
	t.Helper()
	version, err := semver.NewVersion("0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	return &Scroll{
		File: File{
			Name:       "test-scroll",
			Desc:       "test scroll",
			Version:    version,
			AppVersion: "1.0.0",
			Serve:      "start",
			Commands: map[string]*CommandInstructionSet{
				"start": {
					Procedures: []*Procedure{procedure},
				},
			},
		},
		scrollDir: t.TempDir(),
	}
}
