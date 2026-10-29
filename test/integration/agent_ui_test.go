//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/highcard-dev/daemon/apps/druid/adapters/http/handlers"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/core/domain"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	runtimebackend "github.com/highcard-dev/daemon/internal/runtime"
	runtimedocker "github.com/highcard-dev/daemon/internal/runtime/docker"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Opt-in, disposable Docker deployment with an in-memory upload target. It uses
// the real runtime, build toolchain, MCP transport and existing publish worker.
func TestAgentUIBuildAndPublish(t *testing.T) {
	project := os.Getenv("DRUID_UI_TEST_PROJECT")
	buildImage := os.Getenv("DRUID_UI_TEST_BUILD_IMAGE")
	if os.Getenv("DRUID_UI_TEST_DOCKER") != "1" || project == "" || buildImage == "" {
		t.Skip("set DRUID_UI_TEST_DOCKER=1, DRUID_UI_TEST_PROJECT and DRUID_UI_TEST_BUILD_IMAGE for the project's toolchain")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	id := "agent-ui-test-" + uuid.NewString()[:8]
	var mu sync.Mutex
	objects := map[string][]byte{}
	finish := make(chan struct{}, 1)
	uploads := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.URL.Path == "/__finish" {
			select {
			case finish <- struct{}{}:
			default:
			}
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "PUT" {
			data, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
			if err != nil {
				http.Error(w, "upload failed", 500)
				return
			}
			objects[r.URL.Path] = data
			return
		}
		data, ok := objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".wasm") {
			w.Header().Set("Content-Type", "application/wasm")
		} else {
			w.Header().Set("Content-Type", "text/plain")
		}
		_, _ = w.Write(data)
	}))
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	uploads.Listener = listener
	uploads.Start()
	defer uploads.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	state := t.TempDir()
	runtime, err := runtimebackend.NewRuntime("docker", state, runtimebackend.WithDockerConfig(runtimedocker.Config{
		Storage: runtimedocker.StorageBind, BindRoot: filepath.Join(state, "data"),
		UIS3Bucket: "fixture", UIS3Region: "us-east-1", UIS3AccessKey: "fixture", UIS3SecretKey: "fixture",
		UIS3Endpoint: fmt.Sprintf("http://host.docker.internal:%d", port), UIS3PublicBaseURL: base + "/fixture",
	}))
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := appservices.NewRuntimeSupervisor(runtime.Store, coreservices.NewRuntimeScrollManager(runtime.Store), runtime)
	if err != nil {
		t.Fatal(err)
	}
	root := runtime.Backend.RootRef(id, "")
	directory := strings.TrimPrefix(root, "docker-bind://")
	if err := os.MkdirAll(filepath.Join(directory, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	// Copy sources only. Native caches/output in the caller's project are never modified.
	err = filepath.WalkDir(project, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "node_modules" || entry.Name() == "target" || entry.Name() == "dist" || entry.Name() == ".git") {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(project, path)
		if err != nil {
			return err
		}
		target := filepath.Join(directory, "private", rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if entry.Name() == "package-lock.json" {
			data = []byte(strings.ReplaceAll(string(data), "http://127.0.0.1:4873", "http://host.docker.internal:4873"))
		}
		return os.WriteFile(target, data, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	document := `root = Stack([Card("Published component", [TextContent("Real MCP publication verified")], "Disposable integration fixture")])`
	if err := os.WriteFile(filepath.Join(directory, "private/src/view.openui"), []byte(document), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Store.CreateScroll(&domain.RuntimeScroll{ID: id, Root: root, ScrollName: id, ScrollYAML: `name: agent-ui-test
desc: Disposable UI publishing fixture
version: 0.1.0
app_version: test
commands:
  agent:
    run: persistent
    identity:
      permissions: [commands:create, commands:run, queue:read, logs:read, ui:publish]
    procedures:
      - image: alpine:3.20
        command: [sleep, "900"]
`}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := supervisor.DeleteWithPolicy(id, false); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	if err := supervisor.SetWorkloadAPIURL("http://host.docker.internal:8082"); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Run(id, "agent"); err != nil {
		t.Fatal(err)
	}
	token := "" // Docker tool endpoints are intentionally unauthenticated.
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	handlers.RegisterPublicRoutes(app, handlers.RouteHandlers{Server: handlers.NewRuntimeServer(handlers.NewHealthHandler(), handlers.NewScrollHandler(supervisor)), Websocket: handlers.NewWebsocketHandler()})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response, err := app.Test(r, -1)
		if err != nil {
			http.Error(w, "harness failed", 500)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			w.Header()[key] = values
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "agent-ui-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/" + id + "/mcp", HTTPClient: &http.Client{Transport: workloadTransport{token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s: %v %v", name, err, result)
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	call("create_command", map[string]any{"command": "build_fixture", "instructions": map[string]any{"procedures": []any{map[string]any{
		"id": "ui-builder", "image": buildImage, "working_dir": "/workspace/private",
		"mounts":  []any{map[string]any{"path": "/workspace", "sub_path": "."}},
		"command": []string{"sh", "-ec", "npm ci --include=optional && npm run build && test -s dist/app.wasm && test -s dist/app.openui"},
	}}}})
	call("run_command", map[string]any{"command": "build_fixture"})
	for {
		wait := call("wait_for_command", map[string]any{"command": "build_fixture", "timeout_seconds": 30})
		if wait["timed_out"] == true {
			continue
		}
		if wait["status"] != "done" {
			logs := call("read_logs", map[string]any{"console": "ui-builder"})
			t.Fatalf("build failed: %v", logs)
		}
		break
	}
	publication := call("publish_ui", map[string]any{"scope": "private"})
	pkg := publication["package"].(map[string]any)
	url := pkg["url"].(string)
	for _, asset := range []string{url, strings.TrimSuffix(url, ".wasm") + ".openui"} {
		response, err := http.Get(asset)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || len(data) == 0 {
			t.Fatal("published asset unavailable")
		}
	}
	t.Logf("BUILD_AND_REAL_MCP_PUBLISH_OK url=%s finish=%s/__finish", url, base)
	if os.Getenv("DRUID_UI_TEST_BROWSER") == "1" {
		select {
		case <-finish:
		case <-ctx.Done():
			t.Fatal("browser verification timed out")
		}
	}
}
