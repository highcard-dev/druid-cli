package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRouteSplitKeepsManagementAndPublicSurfacesSeparate(t *testing.T) {
	handlers := RouteHandlers{Server: NewRuntimeServer(NewHealthHandler(), nil), Websocket: &WebsocketHandler{}}

	management := fiber.New(fiber.Config{DisableStartupMessage: true})
	RegisterManagementRoutes(management, handlers)
	if status := requestStatus(t, management, "/api/v1/health"); status != http.StatusOK {
		t.Fatalf("management health status = %d, want 200", status)
	}
	if status := requestStatus(t, management, "/scroll-1/api/v1/health"); status != http.StatusNotFound {
		t.Fatalf("management public health status = %d, want 404", status)
	}

	public := fiber.New(fiber.Config{DisableStartupMessage: true})
	RegisterPublicRoutes(public, handlers)
	if status := requestStatus(t, public, "/scroll-1/api/v1/health"); status != http.StatusOK {
		t.Fatalf("public health status = %d, want 200", status)
	}
	if status := requestStatus(t, public, "/api/v1/scrolls"); status != http.StatusNotFound {
		t.Fatalf("public management list status = %d, want 404", status)
	}
	if status := requestStatus(t, public, "/scroll-1/api/v1/token"); status != http.StatusOK {
		t.Fatalf("public token compatibility route status = %d, want 200", status)
	}
}

func TestPublicRoutesAnswerCorsPreflight(t *testing.T) {
	handlers := RouteHandlers{Server: NewRuntimeServer(NewHealthHandler(), nil), Websocket: &WebsocketHandler{}}
	public := fiber.New(fiber.Config{DisableStartupMessage: true})
	RegisterPublicRoutes(public, handlers)

	req := httptest.NewRequest(http.MethodOptions, "/scroll-1/api/v1/watch/status", nil)
	req.Header.Set("Origin", "http://127.0.0.1:3000")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "authorization")
	resp, err := public.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:3000" {
		t.Fatalf("allow origin = %q, want request origin", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow credentials = %q, want true", got)
	}
}

func requestStatus(t *testing.T, app *fiber.App, path string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
