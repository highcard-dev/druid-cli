package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/runtime/docker"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type dockerToolRouteBackend struct{ *toolRouteBackend }

func (*dockerToolRouteBackend) Name() string { return "docker" }

func TestDockerToolsAreAnonymousWhileOwnerRoutesRemainProtected(t *testing.T) {
	store, err := docker.NewStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "other"} {
		if err := store.CreateScroll(&domain.RuntimeScroll{ID: id, Root: t.TempDir(), ScrollName: id, ScrollYAML: "name: fixture\ndesc: fixture\nversion: 0.1.0\napp_version: 1.0\ncommands:\n  echo:\n    procedures:\n      - image: busybox\n"}); err != nil {
			t.Fatal(err)
		}
	}
	backend := &dockerToolRouteBackend{&toolRouteBackend{}}
	supervisor, err := appservices.NewRuntimeSupervisor(store, coreservices.NewRuntimeScrollManager(store), ports.RuntimeBackendFactoryFunc(func(ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) { return backend, nil }), "")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	RegisterPublicRoutes(app, RouteHandlers{Server: NewRuntimeServer(NewHealthHandler(), NewScrollHandler(supervisor)), Websocket: NewWebsocketHandler()})
	for _, path := range []string{"/one/api/v1/scroll"} {
		response, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatalf("owner REST route %s must remain protected: %d", path, response.StatusCode)
		}
	}
	response, err := app.Test(httptest.NewRequest("POST", "/one/api/v1/ui/packages/private/publish", nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("Docker HTTP publication unexpectedly requires credentials")
	}
	response, err = app.Test(httptest.NewRequest("GET", "/one/api/v1/commands", nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("Docker HTTP tools unexpectedly require credentials")
	}
	server := httptest.NewServer(adaptor.FiberApp(app))
	defer server.Close()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "docker-anonymous-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/other/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != len(appservices.ToolPermissions) {
		t.Fatalf("anonymous Docker discovery: %v, %v", tools, err)
	}
	for _, tool := range tools.Tools {
		if _, known := appservices.ToolPermissions[tool.Name]; !known {
			t.Fatalf("unexpected MCP tool %q", tool.Name)
		}
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_command", Arguments: map[string]any{"command": "anonymous-created", "instructions": map[string]any{"procedures": []any{map[string]any{"image": "busybox", "command": []string{"true"}}}}}})
	if err != nil || result.IsError {
		t.Fatalf("anonymous Docker creation: %v, %v", result, err)
	}
}
