//go:build integration

package integration_test

import (
	"bytes"
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
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/highcard-dev/daemon/apps/druid/adapters/http/handlers"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	runtimebackend "github.com/highcard-dev/daemon/internal/runtime"
	runtimedocker "github.com/highcard-dev/daemon/internal/runtime/docker"
	runtimekubernetes "github.com/highcard-dev/daemon/internal/runtime/kubernetes"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type workloadTransport struct{ token string }

func (t workloadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.Header = request.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(copy)
}

// Explicit opt-in keeps this test away from whichever production context the
// developer happens to have selected. It uses only its own temporary namespace
// or containers, and a short-lived HTTP harness, not a second development stack.
func TestWorkloadIdentityRealRuntime(t *testing.T) {
	backendName := os.Getenv("DRUID_WORKLOAD_TEST_RUNTIME")
	if backendName != "docker" && backendName != "kubernetes" {
		t.Skip("set DRUID_WORKLOAD_TEST_RUNTIME=docker or kubernetes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	id := "identity-test-" + uuid.NewString()[:8]
	state := t.TempDir()
	options := []runtimebackend.Option{runtimebackend.WithDockerConfig(runtimedocker.Config{Storage: runtimedocker.StorageBind, BindRoot: filepath.Join(state, "data")})}
	var kubernetes *kubeclient.Clientset
	if backendName == "kubernetes" {
		loading := clientcmd.NewDefaultClientConfigLoadingRules()
		config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, &clientcmd.ConfigOverrides{CurrentContext: "k3d-druid-gs"})
		rest, err := config.ClientConfig()
		if err != nil {
			t.Fatal(err)
		}
		kubernetes, err = kubeclient.NewForConfig(rest)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := kubernetes.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: id}}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = kubernetes.CoreV1().Namespaces().Delete(context.Background(), id, metav1.DeleteOptions{}) })
		// A dedicated kubeconfig prevents the backend from using a different
		// current context than the explicitly selected local test client.
		raw, err := config.RawConfig()
		if err != nil {
			t.Fatal(err)
		}
		raw.CurrentContext = "k3d-druid-gs"
		kubeconfig := filepath.Join(state, "kubeconfig")
		if err := clientcmd.WriteToFile(raw, kubeconfig); err != nil {
			t.Fatal(err)
		}
		options = []runtimebackend.Option{runtimebackend.WithKubernetesConfig(runtimekubernetes.Config{Namespace: id, Kubeconfig: kubeconfig, PullImage: "druid:local"})}
	}
	runtime, err := runtimebackend.NewRuntime(backendName, state, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	supervisor, err := appservices.NewRuntimeSupervisor(runtime.Store, coreservices.NewRuntimeScrollManager(runtime.Store), runtime)
	if err != nil {
		t.Fatal(err)
	}
	root := runtime.Backend.RootRef(id, "")
	if backendName == "kubernetes" {
		pvc := strings.Split(strings.TrimPrefix(root, "k8s://"), "/")[1]
		_, err := kubernetes.CoreV1().PersistentVolumeClaims(id).Create(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: pvc}, Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("32Mi")}}}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
	} else if err := os.MkdirAll(strings.TrimPrefix(root, "docker-bind://"), 0755); err != nil {
		t.Fatal(err)
	}
	scroll := &domain.RuntimeScroll{ID: id, Root: root, ScrollName: id, ScrollYAML: `name: workload-test
desc: Isolated workload identity integration test
version: 0.1.0
app_version: "test"
commands:
  agent:
    run: persistent
    identity:
      permissions: [commands:list, commands:create, queue:read, commands:run, logs:read, console:write, runtime:read]
    procedures:
      - id: agent-console
        image: node:22-alpine
        command: [node, -e, 'const fs=require("node:fs"); const token=fs.readFileSync(process.env.DRUID_TOKEN_FILE,"utf8"); try {fs.writeFileSync(process.env.DRUID_TOKEN_FILE,"bad"); process.exit(9)} catch {} fetch(process.env.DRUID_API_URL+"/api/v1/commands", {headers:{Authorization:"Bearer "+token}}).then(r=>{if(!r.ok)process.exit(8); console.log("MOUNT_API_OK")}).catch(()=>process.exit(7)); require("node:readline").createInterface({input:process.stdin}).on("line",line=>console.log("INPUT:"+line)); console.log("READY");']
`}
	if err := runtime.Store.CreateScroll(scroll); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := supervisor.DeleteWithPolicy(id, false); err != nil {
			t.Errorf("cleanup test runtime: %v", err)
		}
	})
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	handlers.RegisterPublicRoutes(app, handlers.RouteHandlers{Server: handlers.NewRuntimeServer(handlers.NewHealthHandler(), handlers.NewScrollHandler(supervisor)), Websocket: handlers.NewWebsocketHandler()})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response, err := app.Test(r, -1)
		if err != nil {
			http.Error(w, "harness error", 500)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			w.Header()[key] = values
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	defer server.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if err := supervisor.SetWorkloadAPIURL(fmt.Sprintf("http://host.docker.internal:%d", port)); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Run(id, "agent"); err != nil {
		t.Fatal(err)
	}
	registry := runtime.Backend.(ports.CommandIdentityBackend).WorkloadIdentities()
	var token, execution string
	eventually := func(label string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) && ctx.Err() == nil {
			if check() {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("timed out: %s", label)
	}
	eventually("credential provisioning", func() bool {
		records, err := registry.Records(ctx)
		if err != nil {
			return false
		}
		execution = ""
		for _, record := range records {
			if record.DeploymentID == id && record.Command == "agent" {
				execution = record.ExecutionID
				break
			}
		}
		if execution == "" {
			return false
		}
		if kubernetes != nil {
			secret, err := kubernetes.CoreV1().Secrets(id).Get(ctx, "druid-workload-"+execution, metav1.GetOptions{})
			if err != nil {
				return false
			}
			token = string(secret.Data["token"])
		} else {
			data, err := os.ReadFile(filepath.Join(state, "identities", execution))
			if err != nil {
				return false
			}
			token = string(data)
		}
		return token != ""
	})
	call := func(name string, input appservices.ToolInput) (any, error) {
		return supervisor.CallCommandTool(ctx, token, id, name, input)
	}
	eventually("read-only mount and workload-to-host HTTP", func() bool {
		result, err := call("read_logs", appservices.ToolInput{Console: "agent-console"})
		if err != nil {
			return false
		}
		data, _ := json.Marshal(result)
		return bytes.Contains(data, []byte("MOUNT_API_OK"))
	})
	client := mcp.NewClient(&mcp.Implementation{Name: "runtime-integration", Version: "1"}, nil)
	connect := func() *mcp.ClientSession {
		t.Helper()
		session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: fmt.Sprintf("http://127.0.0.1:%d/%s/mcp", port, id), HTTPClient: &http.Client{Transport: workloadTransport{token}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	session := connect()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 9 {
		t.Fatalf("discovery failed: %v", err)
	}
	for _, name := range []string{"list_commands", "get_queue", "get_deployment_status"} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err != nil || result.IsError {
			t.Fatalf("MCP %s failed: %v", name, err)
		}
	}
	if _, err := call("send_stdin", appservices.ToolInput{Console: "agent-console", Text: "exact input\n"}); err != nil {
		t.Fatal(err)
	}
	eventually("stdin echoed and literal log search", func() bool {
		result, err := call("search_logs", appservices.ToolInput{Console: "agent-console", Query: "INPUT:exact input"})
		if err != nil {
			return false
		}
		data, _ := json.Marshal(result)
		return bytes.Contains(data, []byte("INPUT:exact input"))
	})
	created, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_command", Arguments: map[string]any{
		"command": "echo", "instructions": map[string]any{"procedures": []any{map[string]any{
			"id": "echo-console", "image": "node:22-alpine", "command": []string{"node", "-e", `setTimeout(()=>console.log("COMPLETED_AFTER_DISCONNECT"),1000)`},
		}}},
	}})
	if err != nil || created.IsError {
		t.Fatalf("real MCP command creation failed: %v, %v", err, created)
	}
	file, err := supervisor.ScrollFile(id)
	if err != nil || file.Commands["echo"] == nil || file.Commands["echo"].Identity != nil {
		t.Fatal("created command missing or granted identity")
	}
	accepted, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "run_command", Arguments: map[string]any{"command": "echo"}})
	if err != nil || accepted.IsError {
		t.Fatal("MCP enqueue failed")
	}
	var run struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := json.Unmarshal([]byte(accepted.Content[0].(*mcp.TextContent).Text), &run); err != nil {
		t.Fatal(err)
	}
	_ = session.Close() // Accepted work must survive the caller going away.
	if _, err := call("wait_for_command", appservices.ToolInput{Command: "echo", ExecutionID: run.ExecutionID, TimeoutSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	result, err := call("read_logs", appservices.ToolInput{Console: "echo-console"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	if !bytes.Contains(data, []byte("COMPLETED_AFTER_DISCONNECT")) {
		t.Fatal("completed container logs unavailable")
	}
	eventually("healthy identity reconciliation", func() bool { return supervisor.ReconcileCommandIdentities(ctx) == nil })
	if _, err := registry.Lookup(ctx, token); err != nil {
		t.Fatal("surviving execution lost its credential")
	}
	if launcher := os.Getenv("DRUID_WORKLOAD_TEST_LAUNCHER"); launcher != "" {
		bridgeConsole := "bridge-console"
		bridge := &domain.CommandInstructionSet{Run: domain.RunModePersistent, Identity: &domain.CommandIdentity{Permissions: []string{"commands:list", "queue:read"}}, Procedures: []*domain.Procedure{{Id: &bridgeConsole, Image: "node:22-alpine", Command: []string{"node", "-e", launcher}}}}
		if err := supervisor.AddCommand(id, "bridge", bridge); err != nil {
			t.Fatal(err)
		}
		if _, err := supervisor.Run(id, "bridge"); err != nil {
			t.Fatal(err)
		}
		// Exercise the exact SPA-supplied npx launcher and pinned mcp-remote
		// without an LLM. Stdin/stdout carry normal MCP JSON-RPC messages.
		eventually("MCP bridge ready", func() bool {
			result, err := call("read_logs", appservices.ToolInput{Console: "bridge-console"})
			if err != nil {
				return false
			}
			data, _ := json.Marshal(result)
			return bytes.Contains(data, []byte("Proxy established successfully"))
		})
		message := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"integration","version":"1"}}}` + "\n"
		if _, err := call("send_stdin", appservices.ToolInput{Console: "bridge-console", Text: message}); err != nil {
			t.Fatal(err)
		}
		eventually("MCP bridge initialize response", func() bool {
			result, err := call("read_logs", appservices.ToolInput{Console: "bridge-console"})
			if err != nil {
				return false
			}
			data, _ := json.Marshal(result)
			return bytes.Contains(data, []byte("protocolVersion"))
		})
		message = `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n"
		if _, err := call("send_stdin", appservices.ToolInput{Console: "bridge-console", Text: message}); err != nil {
			t.Fatal(err)
		}
		eventually("MCP bridge tool discovery", func() bool {
			result, err := call("read_logs", appservices.ToolInput{Console: "bridge-console"})
			if err != nil {
				return false
			}
			data, _ := json.Marshal(result)
			return bytes.Contains(data, []byte("list_commands"))
		})
		t.Log("verified exact SPA launcher with pinned mcp-remote, mounted token, MCP initialize and tool discovery")
	}
	// Reconnect a new supervisor/registry to persisted running resources.
	reattached, err := runtimebackend.NewRuntime(backendName, state, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reattached.Close() })
	second, err := appservices.NewRuntimeSupervisor(reattached.Store, coreservices.NewRuntimeScrollManager(reattached.Store), reattached)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.SetWorkloadAPIURL(fmt.Sprintf("http://host.docker.internal:%d", port)); err != nil {
		t.Fatal(err)
	}
	if err := second.ReconcileCommandIdentities(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := second.AuthenticateCommand(ctx, token, id); err != nil {
		t.Fatal("daemon reattachment rotated or lost identity")
	}
	definition, err := supervisor.ScrollFile(id)
	if err != nil {
		t.Fatal(err)
	}
	agent := definition.Commands["agent"]
	if err := supervisor.RemoveCommand(id, "agent"); err != nil {
		t.Fatal(err)
	}
	eventually("revocation", func() bool { _, err := registry.Lookup(ctx, token); return err != nil })
	if _, err := call("list_commands", appservices.ToolInput{}); err == nil {
		t.Fatal("removed execution remained authorized")
	}
	oldToken, oldExecution := token, execution
	if err := supervisor.AddCommand(id, "agent", agent); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Run(id, "agent"); err != nil {
		t.Fatal(err)
	}
	eventually("fresh execution rotates credential", func() bool {
		records, err := registry.Records(ctx)
		if err != nil {
			return false
		}
		for _, record := range records {
			if record.DeploymentID != id || record.Command != "agent" || record.ExecutionID == oldExecution {
				continue
			}
			execution = record.ExecutionID
			if kubernetes != nil {
				secret, err := kubernetes.CoreV1().Secrets(id).Get(ctx, "druid-workload-"+execution, metav1.GetOptions{})
				if err != nil {
					return false
				}
				token = string(secret.Data["token"])
			} else {
				data, err := os.ReadFile(filepath.Join(state, "identities", execution))
				if err != nil {
					return false
				}
				token = string(data)
			}
			return token != oldToken && strings.HasPrefix(token, workloadidentity.TokenPrefix)
		}
		return false
	})
	if _, err := registry.Lookup(ctx, oldToken); err == nil {
		t.Fatal("old credential resurrected after replacement")
	}
	t.Log("verified real mount/HTTP, all nine tools, command creation/execution, stdin/logs, completed logs, disconnection, daemon reattachment, and removal revocation")
}
