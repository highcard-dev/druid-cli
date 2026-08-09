package client

import (
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
	"github.com/gorilla/websocket"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

func TestDevCommandExposesFlags(t *testing.T) {
	for _, name := range []string{"watch", "command", "disable", "status", "trigger", "root", "listen"} {
		if flag := DevCommand.Flags().Lookup(name); flag == nil {
			t.Fatalf("druid dev should expose --%s", name)
		}
	}
}

func TestDevServerWebDAVReadWriteAndCallback(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "private"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	runCalls := make(chan struct{}, 4)
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/scrolls/smoke/commands/build" {
			t.Fatalf("unexpected daemon path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing daemon token")
		}
		runCalls <- struct{}{}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"smoke"}`))
	}))
	defer daemon.Close()
	oldURL, oldTokenFile, oldRuntimeID := devDaemonURL, devDaemonTokenFile, devRuntimeID
	devDaemonURL = daemon.URL
	tokenFile := filepath.Join(root, "token")
	if err := os.WriteFile(tokenFile, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	devDaemonTokenFile = tokenFile
	devRuntimeID = "smoke"
	t.Cleanup(func() {
		devDaemonURL, devDaemonTokenFile, devRuntimeID = oldURL, oldTokenFile, oldRuntimeID
	})

	queue := &devTriggerQueue{}
	watch := coreservices.NewDevService(queue, devScrollService{commands: []string{"build"}})
	if err := watch.SetHotReloadCommands([]string{"build"}); err != nil {
		t.Fatal(err)
	}
	if err := watch.StartWatching(root, "private"); err != nil {
		t.Fatal(err)
	}
	defer watch.StopWatching()
	// Starting Developer Mode always builds once before it begins watching edits.
	select {
	case <-runCalls:
	case <-time.After(time.Second):
		t.Fatal("initial build did not run")
	}
	app := newDevApp(root, watch)

	req := httptest.NewRequest(http.MethodPut, "/webdav/private/config.json", strings.NewReader(`{"ok":true}`))
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d", res.StatusCode)
	}
	select {
	case <-runCalls:
	case <-time.After(time.Second):
		t.Fatal("file change did not run a rebuild")
	}
	if got, err := os.ReadFile(filepath.Join(root, "private/config.json")); err != nil || string(got) != `{"ok":true}` {
		t.Fatalf("written file = %q, err = %v", got, err)
	}

	res, err = app.Test(httptest.NewRequest(http.MethodGet, "/webdav/private/config.json", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || string(body) != `{"ok":true}` {
		t.Fatalf("GET status=%d body=%q", res.StatusCode, body)
	}

	res, err = app.Test(httptest.NewRequest(http.MethodHead, "/webdav/private/config.json", nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Length") == "" {
		t.Fatalf("HEAD status=%d content-length=%q", res.StatusCode, res.Header.Get("Content-Length"))
	}

	res, err = app.Test(httptest.NewRequest(http.MethodOptions, "/webdav/private/config.json", nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		t.Fatalf("OPTIONS status=%d", res.StatusCode)
	}
	if res.Header.Get("DAV") == "" {
		t.Fatalf("OPTIONS should be handled by the WebDAV library, DAV header is empty")
	}

	req = httptest.NewRequest("MKCOL", "/webdav/data/folder", nil)
	res, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		t.Fatalf("MKCOL status=%d body=%q", res.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(root, "data/folder")); err != nil {
		t.Fatalf("MKCOL folder missing: %v", err)
	}

	req = httptest.NewRequest("PROPFIND", "/webdav/private/config.json", strings.NewReader(""))
	req.Header.Set("Depth", "0")
	res, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusMultiStatus {
		t.Fatalf("PROPFIND status=%d", res.StatusCode)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/files?path=private/api.txt", strings.NewReader("typed"))
	res, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("typed PUT status=%d", res.StatusCode)
	}
	res, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/files?path=private/api.txt", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || string(body) != "typed" {
		t.Fatalf("typed GET status=%d body=%q", res.StatusCode, body)
	}
}

func TestDevServerWebsocketReceivesWatcherBuildEvents(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "private", "src"), 0755); err != nil {
		t.Fatal(err)
	}

	builds := make(chan struct{}, 4)
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		builds <- struct{}{}
		_, _ = w.Write([]byte(`{"id":"smoke"}`))
	}))
	defer daemon.Close()
	oldURL, oldTokenFile, oldRuntimeID := devDaemonURL, devDaemonTokenFile, devRuntimeID
	devDaemonURL, devDaemonTokenFile, devRuntimeID = daemon.URL, "", "smoke"
	t.Cleanup(func() {
		devDaemonURL, devDaemonTokenFile, devRuntimeID = oldURL, oldTokenFile, oldRuntimeID
	})

	queue := &devTriggerQueue{}
	watch := coreservices.NewDevService(queue, devScrollService{commands: []string{"build"}})
	if err := watch.SetHotReloadCommands([]string{"build"}); err != nil {
		t.Fatal(err)
	}
	if err := watch.StartWatching(root, "private/src"); err != nil {
		t.Fatal(err)
	}
	defer watch.StopWatching()
	select {
	case <-builds:
	case <-time.After(time.Second):
		t.Fatal("initial build did not run")
	}

	app := newDevApp(root, watch)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() { _ = app.Listener(listener) }()
	defer app.Shutdown()

	ws, _, err := websocket.DefaultDialer.Dial("ws://"+listener.Addr().String()+"/ws/v1/watch/notify", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := os.WriteFile(filepath.Join(root, "private", "src", "app.tsx"), []byte("export {}"), 0644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := ws.SetReadDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		_, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			CommandKey string `json:"command_key"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}
		if event.CommandKey == "build-ended" {
			return
		}
	}
	t.Fatal("websocket did not receive build-ended")
}

func TestDevFilePathRejectsTraversal(t *testing.T) {
	if _, err := devFilePath(t.TempDir(), "../escape"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestDevServerFileAuth(t *testing.T) {
	root := t.TempDir()
	watch := coreservices.NewDevService(&devTriggerQueue{}, devScrollService{})
	if err := watch.StartWatching(root, "."); err != nil {
		t.Fatal(err)
	}
	defer watch.StopWatching()
	app := newDevApp(root, watch, devAuth{
		user:      devTestAuth{},
		runtime:   devTestAuth{},
		runtimeID: "smoke",
		ownerID:   "owner",
	})

	res, err := app.Test(httptest.NewRequest(http.MethodPut, "/api/v1/files?path=private/api.txt", strings.NewReader("typed")))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated PUT status=%d", res.StatusCode)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/files?path=private/api.txt", strings.NewReader("typed"))
	req.Header.Set("Authorization", "Bearer user")
	res, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("authenticated PUT status=%d", res.StatusCode)
	}

	res, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/files?path=private/api.txt&token=runtime", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || string(body) != "typed" {
		t.Fatalf("runtime-token GET status=%d body=%q", res.StatusCode, body)
	}
}

type devTestAuth struct{}

func (devTestAuth) CheckHeader(c *fiber.Ctx) (*ports.AuthContext, error) {
	if c.Get(fiber.HeaderAuthorization) != "Bearer user" {
		return nil, fmt.Errorf("missing token")
	}
	return &ports.AuthContext{Subject: "owner"}, nil
}

func (devTestAuth) CheckQuery(runtimeID string, token string) (*ports.AuthContext, error) {
	if runtimeID != "smoke" || token != "runtime" {
		return nil, fmt.Errorf("invalid token")
	}
	return &ports.AuthContext{Subject: "owner", RuntimeID: runtimeID}, nil
}

func (devTestAuth) GenerateQueryToken(string, string) string { return "runtime" }
