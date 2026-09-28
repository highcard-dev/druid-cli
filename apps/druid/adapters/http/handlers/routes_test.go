package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
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

	req := httptest.NewRequest(http.MethodOptions, "/scroll-1/api/v1/scroll", nil)
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

type ownerRouteStore struct{ ports.RuntimeScrollStore }

func (ownerRouteStore) GetScroll(id string) (*domain.RuntimeScroll, error) {
	return &domain.RuntimeScroll{ID: id, OwnerID: "owner"}, nil
}

type ownerRouteAuth struct {
	ports.AuthorizerServiceInterface
}

func (ownerRouteAuth) CheckHeader(c *fiber.Ctx) (*ports.AuthContext, error) {
	if c.Get("Authorization") == "" {
		return nil, nil
	}
	return &ports.AuthContext{Subject: strings.TrimPrefix(c.Get("Authorization"), "Bearer ")}, nil
}

type ownerRouteBackend struct{ ports.RuntimeBackendInterface }

func TestPublicReleaseRoutesEnforceOwnerBeforeReadingOrUpdating(t *testing.T) {
	supervisor, err := appservices.NewRuntimeSupervisor(ownerRouteStore{}, nil, ports.RuntimeBackendFactoryFunc(func(ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
		return ownerRouteBackend{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	RegisterPublicRoutes(app, RouteHandlers{Server: NewRuntimeServer(NewHealthHandler(), NewScrollHandler(supervisor, ownerRouteAuth{})), Websocket: &WebsocketHandler{}})
	for _, route := range []struct{ method, path string }{{http.MethodGet, "/owned/api/v1/release"}, {http.MethodPost, "/owned/api/v1/update"}} {
		for _, auth := range []struct {
			header string
			status int
		}{{"", http.StatusUnauthorized}, {"Bearer other-owner", http.StatusForbidden}} {
			req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{"artifact":"registry/scroll:mutable"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", auth.header)
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != auth.status {
				t.Fatalf("%s %s: got %d, want %d", route.method, route.path, res.StatusCode, auth.status)
			}
		}
	}
	// An owner reaches the explicit-update validator, not a 404 or another API.
	req := httptest.NewRequest(http.MethodPost, "/owned/api/v1/update", strings.NewReader(`{"artifact":"registry/scroll:mutable"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer owner")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("owner mutable update status = %d, want 400", res.StatusCode)
	}
}
