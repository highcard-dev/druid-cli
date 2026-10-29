package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/jsonschema-go/jsonschema"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/runtime/docker"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPublishUIDescriptionRequiresSeparateCustomerApproval(t *testing.T) {
	for _, definition := range commandToolDefinitions() {
		if definition.Name != "publish_ui" {
			continue
		}
		for _, instruction := range []string{"focused previews", "end your turn and wait", "subsequent explicit customer reply", "original UI-change request is not approval", "Revisions require renewed approval", "failed previews", "behavior-only changes", "Do not bypass approval", "not a server-enforced approval check"} {
			if !strings.Contains(definition.Description, instruction) {
				t.Errorf("publish_ui description missing %q", instruction)
			}
		}
		return
	}
	t.Fatal("publish_ui tool is missing")
}

func TestConcreteToolSchemaNormalizesMapValuesWithoutChangingRequiredFields(t *testing.T) {
	schema := &jsonschema.Schema{Type: "object", Required: []string{"required"}, Properties: map[string]*jsonschema.Schema{
		"required": {Types: []string{"string", "null"}},
		"optional": {Type: "object", AdditionalProperties: &jsonschema.Schema{Types: []string{"null", "string"}}},
	}}
	concreteToolSchema(schema)
	if schema.Properties["required"].Type != "string" || schema.Properties["optional"].AdditionalProperties.Type != "string" {
		t.Fatal("nullable scalar or map value lost its concrete type")
	}
	if len(schema.Required) != 1 || schema.Required[0] != "required" {
		t.Fatal("schema normalization changed field optionality")
	}
}

func TestCreateCommandSchemaPreservesNestedTypesForOpenHands(t *testing.T) {
	definition := commandToolDefinitions()[0]
	if definition.Name != "create_command" {
		t.Fatal("creation tool missing")
	}
	instructions := definition.InputSchema.(map[string]any)["properties"].(map[string]any)["instructions"].(*jsonschema.Schema)
	var visit func(*jsonschema.Schema)
	visit = func(schema *jsonschema.Schema) {
		if schema == nil {
			return
		}
		if len(schema.Types) > 0 {
			t.Fatalf("OpenHands loses nested fields for nullable type %v", schema.Types)
		}
		for _, property := range schema.Properties {
			visit(property)
		}
		visit(schema.Items)
		visit(schema.AdditionalProperties)
	}
	visit(instructions)
	procedures := instructions.Properties["procedures"]
	if procedures.Type != "array" || procedures.MinItems == nil || *procedures.MinItems != 1 || procedures.Items.Type != "object" {
		t.Fatal("procedures must advertise a nonempty object array")
	}
	procedure := procedures.Items
	for _, field := range []string{"image", "id", "command", "env", "mounts", "expectedPorts", "target", "signal"} {
		if procedure.Properties[field] == nil {
			t.Fatalf("procedure field %s missing", field)
		}
		if procedure.Properties[field].Description == "" {
			t.Fatalf("Scroll field %s lost its description", field)
		}
	}
	if procedure.Properties["command"].Type != "array" || procedure.Properties["command"].Items.Type != "string" || procedure.Properties["env"].AdditionalProperties.Type != "string" {
		t.Fatal("argument array or environment map schema lost")
	}
	if procedure.Properties["mounts"].Items.Properties["read_only"].Type != "boolean" || procedure.Properties["expectedPorts"].Items.Properties["name"].Type != "string" {
		t.Fatal("mount or port-reference schema lost")
	}
	if instructions.Properties["identity"] != nil || len(procedure.Required) != 0 || len(instructions.Properties["run"].Enum) != 4 {
		t.Fatal("identity exclusion, optional ignore_failure, or run modes changed")
	}
	if procedures.Description == "" || !strings.Contains(instructions.Properties["run"].Description, "NOT a shell command") {
		t.Fatal("Scroll procedure or scheduling-mode documentation lost")
	}
	if !strings.Contains(definition.Description, "Example: ") {
		t.Fatal("model-visible example missing")
	}
}

type toolRouteBackend struct {
	ports.RuntimeBackendInterface
	credentials sync.Map
	logs        string
	uploadError error
}

func (b *toolRouteBackend) PrepareCommandIdentity(context.Context, workloadidentity.Record) error {
	return nil
}
func (b *toolRouteBackend) AuthenticateCommandIdentity(_ context.Context, token string) (workloadidentity.Record, error) {
	if record, ok := b.credentials.Load(token); ok {
		return record.(workloadidentity.Record), nil
	}
	return workloadidentity.Record{}, workloadidentity.ErrUnauthorized
}
func (b *toolRouteBackend) Name() string                                  { return "fixture" }
func (b *toolRouteBackend) RunCommand(ports.RuntimeCommand) (*int, error) { return nil, nil }
func (b *toolRouteBackend) CreateUIPackageUpload(_ context.Context, action ports.RuntimeUIPackageUploadAction) (ports.RuntimeUIPackageUpload, error) {
	if b.uploadError != nil {
		return ports.RuntimeUIPackageUpload{}, b.uploadError
	}
	return ports.RuntimeUIPackageUpload{UploadURL: "http://upload-private/wasm", DocumentUploadURL: "http://upload-private/document", URL: "http://packages/" + string(action.Scope) + "/app.wasm"}, nil
}
func (b *toolRouteBackend) StopCommand(string, string) error { return nil }
func (b *toolRouteBackend) ReadConsoleLogs(context.Context, string, string, int, int64) (string, bool, error) {
	return b.logs, false, nil
}
func (b *toolRouteBackend) WriteConsoleInput(context.Context, string, string, []byte) (int, error) {
	return 0, nil
}

type authenticatedTransport struct{ token string }

func (t authenticatedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.Header = request.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(copy)
}

func TestCommandToolsRealMCPClient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	token := "fixture-token"
	store, err := docker.NewStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := &domain.RuntimeScroll{ID: "tools", Root: t.TempDir(), ScrollName: "tools", ScrollYAML: "name: tools\ndesc: Tools fixture\nversion: 0.1.0\napp_version: 1.0\ncommands:\n  agent:\n    run: persistent\n    identity:\n      permissions: [commands:list, commands:create, queue:read, ui:publish]\n    procedures:\n      - image: busybox\n        env: {SECRET: must-not-leak}\n"}
	runtime.Procedures = domain.ProcedureStatusMap{"agent": {"agent.0": {Status: domain.ScrollLockStatusRunning}}}
	if err := store.CreateScroll(runtime); err != nil {
		t.Fatal(err)
	}
	backend := &toolRouteBackend{}
	backend.credentials.Store(token, workloadidentity.Record{ExecutionID: "execution", DeploymentID: "tools", Command: "agent", Permissions: []string{"commands:list", "commands:create", "queue:read", "ui:publish", "logs:read"}})
	supervisor, err := appservices.NewRuntimeSupervisor(store, coreservices.NewRuntimeScrollManager(store), ports.RuntimeBackendFactoryFunc(func(ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) { return backend, nil }))
	if err != nil {
		t.Fatal(err)
	}
	scrolls := NewScrollHandler(supervisor)
	scrolls.SetAllowUnauthenticatedPublic(true)
	management := fiber.New(fiber.Config{DisableStartupMessage: true})
	RegisterManagementRoutes(management, RouteHandlers{Server: NewRuntimeServer(NewHealthHandler(), scrolls), Websocket: NewWebsocketHandler()})
	managementRequest := httptest.NewRequest("GET", "/api/v1/scrolls", nil)
	managementRequest.Header.Set("Authorization", "Bearer "+string(token))
	managementResponse, err := management.Test(managementRequest)
	if err != nil {
		t.Fatal(err)
	}
	managementResponse.Body.Close()
	if managementResponse.StatusCode != 403 {
		t.Fatal("command credential reached management API without infrastructure authentication")
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	RegisterPublicRoutes(app, RouteHandlers{Server: NewRuntimeServer(NewHealthHandler(), scrolls), Websocket: NewWebsocketHandler()})
	for _, route := range []struct{ method, path string }{
		{"GET", "/:id/api/v1/queue"},
		{"POST", "/:id/api/v1/ui/packages/:scope/publish"},
	} {
		count := 0
		for _, registered := range app.GetRoutes() {
			if registered.Method == route.method && registered.Path == route.path {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("owner REST route %s is registered %d times", route.path, count)
		}
	}
	for _, route := range []struct{ method, path string }{
		{"GET", "/tools/api/v1/commands"},
		{"POST", "/tools/api/v1/commands"},
		{"POST", "/tools/api/v1/commands/agent"},
		{"GET", "/tools/api/v1/commands/agent/executions/execution/wait"},
		{"GET", "/tools/api/v1/consoles/agent.0/logs"},
		{"POST", "/tools/api/v1/consoles/agent.0/logs/search"},
		{"POST", "/tools/api/v1/consoles/agent.0/stdin"},
		{"GET", "/tools/api/v1/status"},
	} {
		response, err := app.Test(httptest.NewRequest(route.method, route.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatalf("removed agent REST route %s: %d", route.path, response.StatusCode)
		}
	}
	for _, path := range []string{"/tools/api/v1/commands", "/other/api/v1/commands", "/tools/api/v1/token", "/tools/api/v1/scroll", "/tools/api/v1/queue", "/tools/api/v1/ui/packages/private/publish"} {
		request := httptest.NewRequest("GET", path, nil)
		request.Header.Set("Authorization", "Bearer "+string(token))
		response, err := app.Test(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode < 400 {
			t.Fatalf("workload reached forbidden route %s", path)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		response, err := app.Test(request, -1)
		if err != nil {
			http.Error(writer, "fixture HTTP failure", 500)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			writer.Header()[key] = values
		}
		writer.WriteHeader(response.StatusCode)
		io.Copy(writer, response.Body)
	}))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "druid-test", Version: "1"}, nil)
	for _, denied := range []struct{ deployment, token string }{{"tools", ""}, {"tools", "invalid"}, {"other", token}} {
		deniedSession, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/" + denied.deployment + "/mcp", HTTPClient: &http.Client{Transport: authenticatedTransport{denied.token}}}, nil)
		if err == nil {
			deniedSession.Close()
			t.Fatal("MCP accepted an absent, invalid or cross-deployment credential")
		}
	}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/tools/mcp", HTTPClient: &http.Client{Transport: authenticatedTransport{string(token)}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 7 {
		t.Fatalf("permission-filtered discovery returned %d tools", len(listed.Tools))
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_commands", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatal("MCP command listing failed")
	}
	t.Run("MCP sanitizes workload logs and tool errors", func(t *testing.T) {
		jwt := "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJmaXh0dXJlIn0.c2ln"
		backend.logs = "application printed " + jwt
		for _, test := range []struct {
			tool      string
			arguments map[string]any
			failure   bool
		}{
			{"read_logs", map[string]any{"console": "agent.0"}, false},
			{"search_logs", map[string]any{"console": "agent.0", "query": "application"}, false},
			{"publish_ui", map[string]any{"scope": "private"}, true},
		} {
			if test.failure {
				backend.uploadError = &appservices.ToolError{Status: 400, Message: "upstream error " + jwt}
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: test.tool, Arguments: test.arguments})
			if err != nil || result.IsError != test.failure || len(result.Content) != 1 {
				t.Fatalf("%s MCP response: %v, %v", test.tool, result, err)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if strings.Contains(text, jwt) || !strings.Contains(text, "[REDACTED]") {
				t.Fatalf("%s did not sanitize workload content", test.tool)
			}
			backend.uploadError = nil
		}
	})
	publication, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "publish_ui", Arguments: map[string]any{"scope": "private"}})
	if err != nil || publication.IsError {
		t.Fatalf("MCP publish failed: %v, %v", err, publication)
	}
	encodedPublication, _ := json.Marshal(publication)
	if !strings.Contains(string(encodedPublication), "published") || strings.Contains(string(encodedPublication), "upload-private") {
		t.Fatal("unsafe publication projection")
	}
	invalid, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_commands", Arguments: map[string]any{"environment": map[string]string{"SECRET": "must-not-be-applied"}}})
	if err == nil && !invalid.IsError {
		t.Fatal("MCP accepted an unknown override field")
	}
	encoded, _ := json.Marshal(result)
	if !strings.Contains(string(encoded), "agent") || strings.Contains(string(encoded), "must-not-leak") || strings.Contains(string(encoded), string(token)) {
		t.Fatal("MCP metadata projection failed")
	}
	var arguments map[string]any
	_, example, found := strings.Cut(commandToolDefinitions()[0].Description, "Example: ")
	if !found || json.Unmarshal([]byte(example), &arguments) != nil {
		t.Fatal("advertised example is not valid JSON")
	}
	created, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_command", Arguments: arguments})
	if err != nil || created.IsError {
		t.Fatalf("MCP creation failed: %v, %v", err, created)
	}
	duplicate, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_command", Arguments: arguments})
	if err == nil && !duplicate.IsError {
		t.Fatal("MCP overwrote an existing command")
	}
	for _, instructions := range []map[string]any{
		{"procedures": []any{map[string]any{"image": "busybox", "command": "echo hello"}}},
		{"procedures": []any{map[string]any{"image": "busybox", "name": "server", "run": "echo hello"}}},
		{"procedures": []any{}},
		{"procedures": nil},
	} {
		invalid, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_command", Arguments: map[string]any{"command": "malformed", "instructions": instructions}})
		if err == nil && !invalid.IsError {
			t.Fatal("MCP accepted malformed command instructions")
		}
	}
	signal, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_command", Arguments: map[string]any{"command": "signal-example", "instructions": map[string]any{"procedures": []any{map[string]any{"type": "signal", "target": "agent.0", "signal": "SIGTERM"}}}}})
	if err != nil || signal.IsError {
		t.Fatalf("signal procedure schema regressed: %v, %v", err, signal)
	}
	// A credential with only commands:list must neither discover nor call creation.
	limitedToken := "limited-token"
	backend.credentials.Store(limitedToken, workloadidentity.Record{ExecutionID: "limited", DeploymentID: "tools", Command: "agent", Permissions: []string{"commands:list"}})
	limitedSession, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/tools/mcp", HTTPClient: &http.Client{Transport: authenticatedTransport{string(limitedToken)}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer limitedSession.Close()
	limitedTools, err := limitedSession.ListTools(ctx, nil)
	if err != nil || len(limitedTools.Tools) != 1 || limitedTools.Tools[0].Name != "list_commands" {
		t.Fatal("creation leaked through filtered discovery")
	}
	deniedPublish, err := limitedSession.CallTool(ctx, &mcp.CallToolParams{Name: "publish_ui", Arguments: map[string]any{"scope": "private"}})
	if err == nil && !deniedPublish.IsError {
		t.Fatal("MCP accepted publishing without permission")
	}
	deniedCreation, err := limitedSession.CallTool(ctx, &mcp.CallToolParams{Name: "create_command", Arguments: arguments})
	if err == nil && !deniedCreation.IsError {
		t.Fatal("MCP accepted creation without its permission")
	}
	backend.credentials.Delete(token)
	if _, err := session.ListTools(ctx, nil); err == nil {
		t.Fatal("MCP discovery accepted a revoked credential")
	}
	// Existing dashboard REST endpoints still work with owner authentication.
	scrolls.authorizer = ownerRouteAuth{}
	for _, route := range []struct{ method, path string }{
		{"GET", "/tools/api/v1/queue"},
		{"POST", "/tools/api/v1/ui/packages/private/publish"},
		{"POST", "/tools/api/v1/ui/packages/public/publish"},
	} {
		request := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer owner")
		response, err := app.Test(request, -1)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("owner REST %s: %d", route.path, response.StatusCode)
		}
	}
}
