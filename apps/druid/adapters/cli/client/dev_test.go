package client

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
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
	if err := os.MkdirAll(filepath.Join(root, "data/private"), 0755); err != nil {
		t.Fatal(err)
	}
	runCalls := 0
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/scrolls/smoke/commands/build" {
			t.Fatalf("unexpected daemon path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing daemon token")
		}
		runCalls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"smoke"}`))
	}))
	defer daemon.Close()
	oldURL, oldToken, oldRuntimeID := devDaemonURL, devDaemonToken, devRuntimeID
	devDaemonURL = daemon.URL
	devDaemonToken = "secret"
	devRuntimeID = "smoke"
	t.Cleanup(func() {
		devDaemonURL, devDaemonToken, devRuntimeID = oldURL, oldToken, oldRuntimeID
	})

	broadcast := domain.NewHub()
	go broadcast.Run()
	app := newDevApp(root, broadcast, &devTriggerQueue{broadcast: broadcast, commands: []string{"build"}})

	req := httptest.NewRequest(http.MethodPut, "/webdav/data/private/config.json", strings.NewReader(`{"ok":true}`))
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d", res.StatusCode)
	}
	if runCalls != 1 {
		t.Fatalf("runCalls = %d, want 1", runCalls)
	}
	if got, err := os.ReadFile(filepath.Join(root, "data/private/config.json")); err != nil || string(got) != `{"ok":true}` {
		t.Fatalf("written file = %q, err = %v", got, err)
	}

	res, err = app.Test(httptest.NewRequest(http.MethodGet, "/webdav/data/private/config.json", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || string(body) != `{"ok":true}` {
		t.Fatalf("GET status=%d body=%q", res.StatusCode, body)
	}

	res, err = app.Test(httptest.NewRequest(http.MethodHead, "/webdav/data/private/config.json", nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Length") == "" {
		t.Fatalf("HEAD status=%d content-length=%q", res.StatusCode, res.Header.Get("Content-Length"))
	}

	res, err = app.Test(httptest.NewRequest(http.MethodOptions, "/webdav/data/private/config.json", nil))
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

	req = httptest.NewRequest("PROPFIND", "/webdav/data/private/config.json", strings.NewReader(""))
	req.Header.Set("Depth", "0")
	res, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusMultiStatus {
		t.Fatalf("PROPFIND status=%d", res.StatusCode)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/files?path=data/private/api.txt", strings.NewReader("typed"))
	res, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("typed PUT status=%d", res.StatusCode)
	}
	res, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/files?path=data/private/api.txt", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || string(body) != "typed" {
		t.Fatalf("typed GET status=%d body=%q", res.StatusCode, body)
	}
}

func TestDevFilePathRejectsTraversal(t *testing.T) {
	if _, err := devFilePath(t.TempDir(), "../escape"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestDevServerFileAuth(t *testing.T) {
	root := t.TempDir()
	broadcast := domain.NewHub()
	go broadcast.Run()
	app := newDevApp(root, broadcast, &devTriggerQueue{broadcast: broadcast}, devAuth{
		user:      devTestAuth{},
		runtime:   devTestAuth{},
		runtimeID: "smoke",
		ownerID:   "owner",
	})

	res, err := app.Test(httptest.NewRequest(http.MethodPut, "/api/v1/files?path=data/private/api.txt", strings.NewReader("typed")))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated PUT status=%d", res.StatusCode)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/files?path=data/private/api.txt", strings.NewReader("typed"))
	req.Header.Set("Authorization", "Bearer user")
	res, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("authenticated PUT status=%d", res.StatusCode)
	}

	res, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/files?path=data/private/api.txt&token=runtime", nil))
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
