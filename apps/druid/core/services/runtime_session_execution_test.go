package services

import (
	"errors"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

func TestRuntimeSessionRunCommandPassesCommandContextToRuntimeBackend(t *testing.T) {
	var seen ports.RuntimeCommand
	session := newRuntimeSessionExecutionTest(t, executionScrollYAML(), &fakeWorkerBackend{
		runCommand: func(command ports.RuntimeCommand) (*int, error) {
			seen = command
			return nil, nil
		},
	})

	if err := session.runCommand("serve"); err != nil {
		t.Fatal(err)
	}

	if seen.Name != "serve" {
		t.Fatalf("Name = %s, want serve", seen.Name)
	}
	if seen.Command == nil || seen.Command.Run != domain.RunModePersistent {
		t.Fatalf("Command = %#v, want persistent instruction set", seen.Command)
	}
	if seen.Root != session.runtimeScroll.Root {
		t.Fatalf("Root = %s, want %s", seen.Root, session.runtimeScroll.Root)
	}
	if len(seen.GlobalPorts) != 1 || seen.GlobalPorts[0].Name != "http" {
		t.Fatalf("GlobalPorts = %#v", seen.GlobalPorts)
	}
}

func TestRuntimeSessionRunCommandPassesRoutingAndScrollIdentity(t *testing.T) {
	var seen ports.RuntimeCommand
	session := newRuntimeSessionExecutionTest(t, executionScrollYAML(), &fakeWorkerBackend{
		runCommand: func(command ports.RuntimeCommand) (*int, error) {
			seen = command
			return nil, nil
		},
	})
	session.runtimeScroll.Routing = []domain.RuntimeRouteAssignment{{
		Name:       "web-http",
		PortName:   "http",
		Host:       "web.example.test",
		ExternalIP: "192.0.2.10",
		PublicPort: 443,
		URL:        "https://web.example.test",
	}}

	if err := session.runCommand("serve"); err != nil {
		t.Fatal(err)
	}

	if seen.ScrollID != "scroll-a" {
		t.Fatalf("ScrollID = %s, want scroll-a", seen.ScrollID)
	}
	if len(seen.Routing) != 1 || seen.Routing[0].PublicPort != 443 {
		t.Fatalf("Routing = %#v", seen.Routing)
	}
	env := seen.ProcedureEnv["web"]
	if env["DRUID_SCROLL_ID"] != "scroll-a" || env["DRUID_SCROLL_NAME"] != "scroll-name" {
		t.Fatalf("scroll env = %#v", env)
	}
	if env["DRUID_RUNTIME_BACKEND"] != "fake-worker" {
		t.Fatalf("backend env = %#v", env)
	}
	if env["DRUID_PORT_HTTP_PUBLIC"] != "443" || env["DRUID_PORT_HTTP_HOST"] != "web.example.test" || env["DRUID_PORT_HTTP_URL"] != "https://web.example.test" {
		t.Fatalf("routing env = %#v", env)
	}
	if env["DRUID_IP"] != "192.0.2.10" || env["DRUID_PORT_HTTP_IP"] != "192.0.2.10" {
		t.Fatalf("ip env = %#v", env)
	}
	if _, ok := env["DRUID_IP_WAIT"]; ok {
		t.Fatalf("DRUID_IP_WAIT should not be set after routing: %#v", env)
	}
}

func TestRuntimeSessionRunCommandPersistsProcedureStatusCallbacks(t *testing.T) {
	exitCode := 0
	session := newRuntimeSessionExecutionTest(t, executionScrollYAML(), &fakeWorkerBackend{
		runCommand: func(command ports.RuntimeCommand) (*int, error) {
			command.ObserveProcedureStatus("web", domain.ScrollLockStatusRunning, nil)
			command.ObserveProcedureStatus("web", domain.ScrollLockStatusDone, &exitCode)
			return nil, nil
		},
	})

	if err := session.runCommand("serve"); err != nil {
		t.Fatal(err)
	}

	updated, err := session.store.GetScroll(session.runtimeScroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	status := updated.Procedures["serve"]["web"]
	if status.Status != domain.ScrollLockStatusDone || status.ExitCode == nil || *status.ExitCode != 0 {
		t.Fatalf("procedure status = %#v, want done with exit 0", status)
	}
}

func TestRuntimeSessionRunCommandMarksProceduresErrorOnEnvBuildFailure(t *testing.T) {
	session := newRuntimeSessionExecutionTest(t, duplicatePortScrollYAML(), &fakeWorkerBackend{})

	if err := session.runCommand("serve"); err == nil {
		t.Fatal("expected env build failure")
	}

	updated, err := session.store.GetScroll(session.runtimeScroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	status := updated.Procedures["serve"]["web"]
	if status.Status != domain.ScrollLockStatusError {
		t.Fatalf("procedure status = %#v, want error", status)
	}
}

func TestRuntimeSessionRunCommandMarksProceduresErrorOnBackendFailure(t *testing.T) {
	exitCode := 23
	session := newRuntimeSessionExecutionTest(t, executionScrollYAML(), &fakeWorkerBackend{
		runCommand: func(command ports.RuntimeCommand) (*int, error) {
			return &exitCode, errors.New("backend failed")
		},
	})

	if err := session.runCommand("serve"); err == nil {
		t.Fatal("expected backend failure")
	}

	updated, err := session.store.GetScroll(session.runtimeScroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	status := updated.Procedures["serve"]["web"]
	if status.Status != domain.ScrollLockStatusError || status.ExitCode == nil || *status.ExitCode != 23 {
		t.Fatalf("procedure status = %#v, want error with exit 23", status)
	}
}

func newRuntimeSessionExecutionTest(t *testing.T, scrollYAML string, backend *fakeWorkerBackend) *RuntimeSession {
	t.Helper()
	store := newTestStateStore(t)
	runtimeScroll := &domain.RuntimeScroll{
		ID:         "scroll-a",
		Artifact:   "local",
		Root:       t.TempDir(),
		ScrollName: "scroll-name",
		ScrollYAML: scrollYAML,
		Procedures: domain.ProcedureStatusMap{},
	}
	if err := store.CreateScroll(runtimeScroll); err != nil {
		t.Fatal(err)
	}
	session, err := NewRuntimeSession(store, runtimeScroll, backend)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func executionScrollYAML() string {
	return `name: scroll-name
desc: Runtime session execution test
version: 0.1.0
app_version: "1.0"
ports:
  - name: http
    protocol: http
    port: 8080
serve: serve
commands:
  serve:
    run: persistent
    procedures:
      - id: web
        image: alpine:3.20
        env:
          APP_ENV: test
          DRUID_PORT_HTTP: user-value
          DRUID_SCROLL_NAME: user-name
`
}

func duplicatePortScrollYAML() string {
	return `name: scroll-name
desc: Runtime session execution test
version: 0.1.0
app_version: "1.0"
ports:
  - name: web-port
    port: 8080
  - name: web_port
    port: 8081
serve: serve
commands:
  serve:
    procedures:
      - id: web
        image: alpine:3.20
`
}
