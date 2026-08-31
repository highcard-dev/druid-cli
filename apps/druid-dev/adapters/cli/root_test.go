package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/devapi"
)

type testAuthorizer struct {
	context *ports.AuthContext
	err     error
}

func (a testAuthorizer) CheckHeader(*fiber.Ctx) (*ports.AuthContext, error) { return a.context, a.err }
func (a testAuthorizer) CheckQuery(string, string) (*ports.AuthContext, error) {
	return a.context, a.err
}
func (a testAuthorizer) GenerateQueryToken(string, string) string { return "token" }

func TestSafePathStaysBelowMountedRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := safePath(root, "private/package.json", false); err != nil {
		t.Fatalf("safe path rejected: %v", err)
	}
	for _, path := range []string{"", "..", "../private/package.json", "/../private/package.json"} {
		if _, err := safePath(root, path, false); err == nil {
			t.Fatalf("unsafe path %q was accepted", path)
		}
	}
}

func TestWatchEnableValidatesRequest(t *testing.T) {
	server := newDevServer(t.TempDir(), devAuth{})
	app := newApp(server)
	t.Cleanup(func() { _ = server.stopWatch() })

	request := devapi.WatchModeRequest{WatchPaths: []string{"../private"}, WorkingDirectory: ".", Command: []string{"sh", "-c", "sleep 1"}}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	httpRequest := httptest.NewRequest(http.MethodPost, "/api/v1/watch/enable", bytes.NewReader(body)).WithContext(t.Context())
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := app.Test(httpRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusBadRequest)
	}
}

func TestFileAccessRequiresTheRuntimeOwner(t *testing.T) {
	root := t.TempDir()
	server := newDevServer(root, devAuth{user: testAuthorizer{context: &ports.AuthContext{Subject: "other-user"}}, ownerID: "owner"})
	app := newApp(server)
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/files?path=private/package.json", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusForbidden)
	}

	server = newDevServer(root, devAuth{user: testAuthorizer{err: errors.New("invalid token")}, ownerID: "owner"})
	app = newApp(server)
	response, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/files?path=private/package.json", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestWatchEnableReusesUnchangedConfiguration(t *testing.T) {
	server := newDevServer(t.TempDir(), devAuth{})
	app := newApp(server)
	t.Cleanup(func() { _ = server.stopWatch() })
	request := devapi.WatchModeRequest{WatchPaths: []string{"."}, WorkingDirectory: ".", Command: []string{"sh", "-c", "sleep 5"}}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		httpRequest := httptest.NewRequest(http.MethodPost, "/api/v1/watch/enable", bytes.NewReader(body)).WithContext(t.Context())
		httpRequest.Header.Set("Content-Type", "application/json")
		response, err := app.Test(httpRequest)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != fiber.StatusOK {
			response.Body.Close()
			t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusOK)
		}
		response.Body.Close()
	}
	if got := server.watchResponse().Status; got != devapi.Ready {
		t.Fatalf("watch status = %q, want ready", got)
	}
}
