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
	if status := requestStatus(t, public, "/scroll-1/api/v1/token"); status != http.StatusNotFound {
		t.Fatalf("public token compatibility route status = %d, want 404", status)
	}
	if status := requestStatus(t, public, "/scroll-1/api/v1/watch/status"); status != http.StatusNotFound {
		t.Fatalf("public watch compatibility route status = %d, want 404", status)
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
