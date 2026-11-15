package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
)

func TestCreateCommandPersistsWithoutRunningAndCanExecute(t *testing.T) {
	s, _, token := commandToolsFixture(t, "commands:create", "commands:list", "commands:run", "queue:read")
	ctx := context.Background()
	input := ToolInput{Command: "created", Instructions: &domain.CommandInstructionSet{Procedures: []*domain.Procedure{{Image: "busybox", Command: []string{"true"}}}}}
	result, err := s.CallCommandTool(ctx, token, "tools", "create_command", input)
	if err != nil || !strings.Contains(fmtJSON(t, result), `"created":true`) {
		t.Fatalf("creation failed: %v", err)
	}
	if _, exists := s.sessions["tools"].Snapshot()["created"]; exists {
		t.Fatal("creation started an execution")
	}
	stored, err := s.store.GetScroll("tools")
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewRuntimeSession(s.store, stored, s.sessions["tools"].runtimeBackend)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.scrollService.GetFile().Commands["created"] == nil || reloaded.scrollService.GetFile().Commands["created"].Identity != nil {
		t.Fatal("definition did not persist without identity")
	}
	listed, err := s.CallCommandTool(ctx, token, "tools", "list_commands", ToolInput{})
	if err != nil || !strings.Contains(fmtJSON(t, listed), "created") {
		t.Fatal("new command was not listed")
	}
	run, err := s.CallCommandTool(ctx, token, "tools", "run_command", ToolInput{Command: "created"})
	if err != nil || run.(map[string]any)["accepted"] != true {
		t.Fatal(err)
	}
	_, err = s.CallCommandTool(ctx, token, "tools", "wait_for_command", ToolInput{Command: "created", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateCommandRejectionsAndConcurrentDuplicates(t *testing.T) {
	s, backend, token := commandToolsFixture(t, "commands:create")
	ctx := context.Background()
	definition := &domain.CommandInstructionSet{Procedures: []*domain.Procedure{{Image: "busybox", Command: []string{"true"}}}}
	for _, test := range []struct {
		input  ToolInput
		status int
	}{
		{ToolInput{Command: "agent", Instructions: definition}, 409},
		{ToolInput{Instructions: definition}, 400},
		{ToolInput{Command: "missing"}, 400},
		{ToolInput{Command: "invalid", Instructions: &domain.CommandInstructionSet{}}, 400},
		{ToolInput{Command: "identity", Instructions: &domain.CommandInstructionSet{Procedures: definition.Procedures, Identity: &domain.CommandIdentity{}}}, 400},
	} {
		_, err := s.CallCommandTool(ctx, token, "tools", "create_command", test.input)
		var failure *ToolError
		if !errors.As(err, &failure) || failure.Status != test.status {
			t.Fatalf("%s: expected %d, got %v", test.input.Command, test.status, err)
		}
	}
	if _, err := backend.AuthenticateCommandIdentity(ctx, token); err != nil {
		t.Fatal("duplicate creation disturbed existing workload credentials")
	}
	if _, err := s.CallCommandTool(ctx, token, "other", "create_command", ToolInput{Command: "new", Instructions: definition}); !errors.Is(err, workloadidentity.ErrUnauthorized) {
		t.Fatal("cross-deployment creation accepted")
	}
	var wg sync.WaitGroup
	errorsCh := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CallCommandTool(ctx, token, "tools", "create_command", ToolInput{Command: "concurrent", Instructions: definition})
			errorsCh <- err
		}()
	}
	wg.Wait()
	close(errorsCh)
	created := 0
	for err := range errorsCh {
		if err == nil {
			created++
		} else {
			var failure *ToolError
			if !errors.As(err, &failure) || failure.Status != 409 {
				t.Fatalf("unexpected concurrent error: %v", err)
			}
		}
	}
	if created != 1 {
		t.Fatalf("created %d commands, expected exactly one", created)
	}
	denied, _, deniedToken := commandToolsFixture(t, "commands:run")
	if _, err := denied.CallCommandTool(ctx, deniedToken, "tools", "create_command", ToolInput{Command: "new", Instructions: definition}); err == nil {
		t.Fatal("commands:run granted command creation")
	}
}

func fmtJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPublishUIToolPermissionsProjectionAndPaths(t *testing.T) {
	s, backend, token := commandToolsFixture(t, "ui:publish")
	s.sessions["tools"].Start()
	ctx := context.Background()
	var commands []string
	backend.runCommand = func(command ports.RuntimeCommand) (*int, error) {
		commands = append(commands, command.Name)
		return nil, nil
	}
	for _, input := range []ToolInput{{Scope: "private"}, {Scope: "public"}, {Scope: "private"}} {
		result, err := s.CallCommandTool(ctx, token, "tools", "publish_ui", input)
		if err != nil {
			t.Fatal(err)
		}
		data := fmtJSON(t, result)
		if !strings.Contains(data, `"published":true`) || !strings.Contains(data, `"updated_at"`) || strings.Contains(data, "uploads/") || strings.Contains(data, "ScrollYAML") || strings.Contains(data, token) {
			t.Fatalf("unsafe or incomplete publication response: %s", data)
		}
		wantPath := input.Scope + "/dist/app.wasm"
		if !strings.Contains(data, wantPath) {
			t.Fatal("wrong published path")
		}
	}
	for _, command := range commands {
		if !strings.HasPrefix(command, "ui_publish_") {
			t.Fatalf("publication unexpectedly built: %s", command)
		}
	}
	for _, input := range []ToolInput{{}, {Scope: "other"}} {
		_, err := s.CallCommandTool(ctx, token, "tools", "publish_ui", input)
		var failure *ToolError
		if !errors.As(err, &failure) || failure.Status != 400 {
			t.Fatalf("invalid request accepted: %#v, %v", input, err)
		}
	}
	if _, err := s.CallCommandTool(ctx, token, "other", "publish_ui", ToolInput{Scope: "private"}); !errors.Is(err, workloadidentity.ErrUnauthorized) {
		t.Fatal("cross-deployment publish accepted")
	}
	previous, _ := s.UIPackages("tools")
	backend.runCommand = func(ports.RuntimeCommand) (*int, error) { return nil, errors.New("fixture upload failed") }
	if _, err := s.CallCommandTool(ctx, token, "tools", "publish_ui", ToolInput{Scope: "private"}); err == nil {
		t.Fatal("upload failure ignored")
	}
	current, _ := s.UIPackages("tools")
	if current[domain.RuntimeUIPackageScopePrivate] != previous[domain.RuntimeUIPackageScopePrivate] {
		t.Fatal("failed publish replaced previous package")
	}
	backend.credentials.Delete(token)
	if _, err := s.CallCommandTool(ctx, token, "tools", "publish_ui", ToolInput{Scope: "private"}); !errors.Is(err, workloadidentity.ErrUnauthorized) {
		t.Fatal("revoked publish accepted")
	}
	denied, _, deniedToken := commandToolsFixture(t, "commands:run")
	if _, err := denied.CallCommandTool(ctx, deniedToken, "tools", "publish_ui", ToolInput{Scope: "private"}); err == nil {
		t.Fatal("commands:run granted publishing")
	}
}

type toolTestBackend struct {
	*fakeWorkerBackend
	credentials     sync.Map
	authentications atomic.Int32
	logs            string
	input           string
	stopped         string
	stopError       error
}

func (b *toolTestBackend) StopCommand(_ string, command string) error {
	b.stopped = command
	return b.stopError
}

func TestRemoveCommandStopsAndPersistsRemoval(t *testing.T) {
	supervisor, backend, token := commandToolsFixture(t, "commands:remove")
	result, err := supervisor.CallCommandTool(context.Background(), token, "tools", "remove_command", ToolInput{Command: "agent"})
	if err != nil || result.(map[string]any)["removed"] != true || backend.stopped != "agent" {
		t.Fatalf("command removal failed: %v, %v", result, err)
	}
	stored, err := supervisor.store.GetScroll("tools")
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewRuntimeSession(supervisor.store, stored, backend)
	if err != nil || reloaded.scrollService.GetFile().Commands["agent"] != nil || reloaded.Snapshot()["agent"] != nil || reloaded.scrollService.GetFile().Commands["echo"] == nil {
		t.Fatal("removal did not persist or changed another command")
	}
}

func TestRemoveCommandPermissionsBindingValidationAndStopFailure(t *testing.T) {
	ctx := context.Background()
	supervisor, backend, token := commandToolsFixture(t, "commands:list")
	if _, err := supervisor.CallCommandTool(ctx, token, "tools", "remove_command", ToolInput{Command: "agent"}); err == nil || backend.stopped != "" {
		t.Fatal("removal bypassed permission checks")
	}
	backend.credentials.Store(token, workloadidentity.Identity{DeploymentID: "tools", Permissions: []string{"commands:remove"}})
	if _, err := supervisor.CallCommandTool(ctx, token, "other", "remove_command", ToolInput{Command: "agent"}); !errors.Is(err, workloadidentity.ErrUnauthorized) || backend.stopped != "" {
		t.Fatal("removal crossed deployment binding")
	}
	if _, err := supervisor.CallCommandTool(ctx, token, "tools", "remove_command", ToolInput{}); err == nil || backend.stopped != "" {
		t.Fatal("removal accepted an empty command")
	}
	backend.stopError = errors.New("fixture stop failed")
	if _, err := supervisor.CallCommandTool(ctx, token, "tools", "remove_command", ToolInput{Command: "agent"}); !errors.Is(err, backend.stopError) {
		t.Fatal("stop failure was not propagated")
	}
	if supervisor.sessions["tools"].scrollService.GetFile().Commands["agent"] == nil {
		t.Fatal("failed stop removed the definition")
	}
}

func (b *toolTestBackend) AuthenticateCommandIdentity(_ context.Context, token string) (workloadidentity.Identity, error) {
	if record, ok := b.credentials.Load(token); ok {
		b.authentications.Add(1)
		return record.(workloadidentity.Identity), nil
	}
	return workloadidentity.Identity{}, workloadidentity.ErrUnauthorized
}
func (b *toolTestBackend) ReadConsoleLogs(_ context.Context, _, _ string, _ int, _ int64) (string, bool, error) {
	return b.logs, false, nil
}
func (b *toolTestBackend) WriteConsoleInput(_ context.Context, _, _ string, data []byte) (int, error) {
	b.input = string(data)
	return len(data), nil
}
func (b *toolTestBackend) Create(observer ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
	b.procedureStatusObserver = observer
	return b, nil
}

func commandToolsFixture(t *testing.T, permissions ...string) (*RuntimeSupervisor, *toolTestBackend, string) {
	t.Helper()
	backend := &toolTestBackend{fakeWorkerBackend: &fakeWorkerBackend{}, logs: "hello\nERROR fixture\n"}
	store := newTestStateStore(t)
	runtime := &domain.RuntimeScroll{ID: "tools", Root: t.TempDir(), ScrollName: "tools", ScrollYAML: `name: tools
desc: Tools fixture
version: 0.1.0
app_version: "1.0"
ui:
  private: {wasm: private/dist/app.wasm, openui: private/view.openui}
  public: {wasm: public/dist/app.wasm, openui: public/view.openui}
commands:
  agent:
    run: persistent
    identity:
      permissions: [commands:list]
    procedures:
      - id: agent-console
        image: busybox
        env: {SECRET: never-expose-this}
        command: [sh, -c, private-shell-argument]
  echo:
    procedures:
      - image: busybox
        command: [true]
`, Procedures: domain.ProcedureStatusMap{"agent": {"agent-console": {Status: domain.ScrollLockStatusRunning}}}}
	if err := store.CreateScroll(runtime); err != nil {
		t.Fatal(err)
	}
	supervisor := newRuntimeSupervisorForTest(t, store, coreservices.NewRuntimeScrollManager(store), backend)
	session, err := NewRuntimeSession(store, runtime, backend)
	if err != nil {
		t.Fatal(err)
	}
	supervisor.sessions[runtime.ID] = session
	t.Cleanup(session.stopDeploymentQueue)
	token := "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJmaXh0dXJlIn0.c2ln"
	backend.credentials.Store(token, workloadidentity.Identity{DeploymentID: "tools", Permissions: permissions})
	return supervisor, backend, token
}

func TestCommandToolsPermissionsProjectionAndConsoles(t *testing.T) {
	supervisor, backend, token := commandToolsFixture(t, "commands:list", "logs:read", "console:write")
	ctx := context.Background()
	listed, err := supervisor.CallCommandTool(ctx, token, "tools", "list_commands", ToolInput{})
	if err != nil {
		t.Fatal(err)
	}
	text := fmtJSON(t, listed)
	if strings.Contains(text, "never-expose") || strings.Contains(text, "private-shell") || strings.Contains(text, token) {
		t.Fatal("private command fields exposed")
	}
	if _, err := supervisor.CallCommandTool(ctx, token, "other", "list_commands", ToolInput{}); !errors.Is(err, workloadidentity.ErrUnauthorized) {
		t.Fatal("cross-deployment request accepted")
	}
	if _, err := supervisor.CallCommandTool(ctx, token, "tools", "get_queue", ToolInput{}); err == nil {
		t.Fatal("missing permission accepted")
	}
	backend.logs += token
	result, err := supervisor.CallCommandTool(ctx, token, "tools", "read_logs", ToolInput{Console: "agent-console"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmtJSON(t, result), token) || !strings.Contains(fmtJSON(t, result), "[REDACTED]") {
		t.Fatal("token not redacted")
	}
	result, err = supervisor.CallCommandTool(ctx, token, "tools", "search_logs", ToolInput{Console: "agent-console", Query: "error"})
	if err != nil || !strings.Contains(fmtJSON(t, result), "ERROR fixture") {
		t.Fatal("literal search failed")
	}
	if _, err := supervisor.CallCommandTool(ctx, token, "tools", "send_stdin", ToolInput{Console: "agent-console", Text: "list\n"}); err != nil || backend.input != "list\n" {
		t.Fatal("exact stdin bytes were not written")
	}
	if _, err := supervisor.CallCommandTool(ctx, token, "tools", "read_logs", ToolInput{Console: "agent-console", TailLines: 1001}); err == nil {
		t.Fatal("unbounded tail accepted")
	}
	if _, err := supervisor.CallCommandTool(ctx, token, "tools", "send_stdin", ToolInput{Console: "agent-console", Text: strings.Repeat("a", 8193)}); err == nil {
		t.Fatal("oversized stdin accepted")
	}
	backend.credentials.Delete(token)
	if _, err := supervisor.CallCommandTool(ctx, token, "tools", "list_commands", ToolInput{}); !errors.Is(err, workloadidentity.ErrUnauthorized) {
		t.Fatal("revoked token accepted")
	}
}

func TestCommandToolsEnqueueAndWaitForCurrentCommand(t *testing.T) {
	supervisor, _, token := commandToolsFixture(t, "commands:run", "queue:read")
	ctx := context.Background()
	result, err := supervisor.CallCommandTool(ctx, token, "tools", "run_command", ToolInput{Command: "echo"})
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["accepted"] != true || result.(map[string]any)["command"] != "echo" {
		t.Fatal("command was not accepted")
	}
	waited, err := supervisor.CallCommandTool(ctx, token, "tools", "wait_for_command", ToolInput{Command: "echo", TimeoutSeconds: 1})
	if err != nil || waited.(map[string]any)["timed_out"] != false {
		t.Fatalf("execution did not complete: %v", err)
	}
	if _, err := supervisor.CallCommandTool(ctx, token, "tools", "wait_for_command", ToolInput{Command: "missing", TimeoutSeconds: 1}); err == nil {
		t.Fatal("missing command accepted")
	}
	if _, err := supervisor.CallCommandTool(ctx, token, "tools", "run_command", ToolInput{Command: "unknown"}); err == nil {
		t.Fatal("unknown command accepted")
	}
}

func TestCommandWaitReturnsCurrentStatusOnTimeout(t *testing.T) {
	supervisor, _, token := commandToolsFixture(t, "queue:read")
	result, err := supervisor.CallCommandTool(context.Background(), token, "tools", "wait_for_command", ToolInput{Command: "agent", TimeoutSeconds: 1})
	if err != nil || result.(map[string]any)["timed_out"] != true || result.(map[string]any)["status"] != domain.ScrollLockStatusRunning {
		t.Fatalf("wait did not return the current running status: %v, %v", result, err)
	}
}

func TestCommandWaitAuthenticatesOnceAndRejectsSubsequentRevokedRequests(t *testing.T) {
	supervisor, backend, token := commandToolsFixture(t, "queue:read")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := supervisor.CallCommandTool(ctx, token, "tools", "wait_for_command", ToolInput{Command: "agent", TimeoutSeconds: 1})
		done <- err
	}()
	for backend.authentications.Load() == 0 {
		if ctx.Err() != nil {
			t.Fatal("wait was not accepted")
		}
		time.Sleep(time.Millisecond)
	}
	backend.credentials.Delete(token)
	// Keep the wait pending beyond a poll, then complete its accepted execution.
	time.Sleep(150 * time.Millisecond)
	supervisor.sessions["tools"].SetCommandStatus("agent", domain.ScrollLockStatusDone, nil)
	if err := <-done; err != nil || backend.authentications.Load() != 1 {
		t.Fatalf("accepted wait repeated authentication: %v", err)
	}
	if _, err := supervisor.CallCommandTool(ctx, token, "tools", "get_queue", ToolInput{}); !errors.Is(err, workloadidentity.ErrUnauthorized) {
		t.Fatal("new request accepted a revoked credential")
	}
}

func TestProcedureEventsUpdateCurrentCommandStatus(t *testing.T) {
	supervisor, _, _ := commandToolsFixture(t)
	session := supervisor.sessions["tools"]
	session.SetCommandStatus("agent", domain.ScrollLockStatusWaiting, nil)
	supervisor.ObserveProcedureStatus(ports.ProcedureStatusUpdate{RuntimeID: "tools", Command: "agent", Procedure: "agent-console", Status: domain.ScrollLockStatusError})
	status := session.Snapshot()["agent"]["agent-console"]
	if status.Status != domain.ScrollLockStatusError {
		t.Fatal("procedure event did not update the named command")
	}
}
