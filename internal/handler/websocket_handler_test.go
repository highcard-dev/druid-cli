package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

// WebsocketTestContext holds all mocked services for websocket handler testing
type WebsocketTestContext struct {
	App               *fiber.App
	Ctrl              *gomock.Controller
	AuthorizerService *mock_ports.MockAuthorizerServiceInterface
	ScrollService     *mock_ports.MockScrollServiceInterface
	ConsoleService    *mock_ports.MockConsoleManagerInterface
	Handler           *WebsocketHandler
}

// setupWebsocketTestApp creates a Fiber app with mocked dependencies for testing
func setupWebsocketTestApp(t *testing.T) *WebsocketTestContext {
	ctrl := gomock.NewController(t)

	authorizerService := mock_ports.NewMockAuthorizerServiceInterface(ctrl)
	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	consoleService := mock_ports.NewMockConsoleManagerInterface(ctrl)

	handler := NewWebsocketHandler(authorizerService, scrollService, consoleService)

	app := fiber.New()
	app.Get("/api/v1/token", handler.CreateToken)
	app.Get("/api/v1/consoles", handler.Consoles)

	return &WebsocketTestContext{
		App:               app,
		Ctrl:              ctrl,
		AuthorizerService: authorizerService,
		ScrollService:     scrollService,
		ConsoleService:    consoleService,
		Handler:           handler,
	}
}

func TestWebsocketHandler_CreateToken_Success(t *testing.T) {
	tc := setupWebsocketTestApp(t)
	defer tc.Ctrl.Finish()

	expectedToken := "test-token-12345"
	tc.AuthorizerService.EXPECT().GenerateQueryToken().Return(expectedToken)

	req := httptest.NewRequest("GET", "/api/v1/token", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.TokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if result.Token != expectedToken {
		t.Errorf("Expected token '%s', got '%s'", expectedToken, result.Token)
	}
}

func TestWebsocketHandler_CreateToken_EmptyToken(t *testing.T) {
	tc := setupWebsocketTestApp(t)
	defer tc.Ctrl.Finish()

	tc.AuthorizerService.EXPECT().GenerateQueryToken().Return("")

	req := httptest.NewRequest("GET", "/api/v1/token", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.TokenResponse
	json.Unmarshal(body, &result)

	if result.Token != "" {
		t.Errorf("Expected empty token, got '%s'", result.Token)
	}
}

func TestWebsocketHandler_Consoles_Success(t *testing.T) {
	tc := setupWebsocketTestApp(t)
	defer tc.Ctrl.Finish()

	expectedConsoles := map[string]*domain.Console{
		"start.0": {
			InputMode: "stdin",
		},
		"worker.0": {
			InputMode: "rcon",
		},
	}
	tc.ConsoleService.EXPECT().GetConsoles().Return(expectedConsoles)

	req := httptest.NewRequest("GET", "/api/v1/consoles", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.ConsolesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(result.Consoles) != 2 {
		t.Errorf("Expected 2 consoles, got %d", len(result.Consoles))
	}
	if _, ok := result.Consoles["start.0"]; !ok {
		t.Error("Expected 'start.0' console to be present")
	}
}

func TestWebsocketHandler_Consoles_Empty(t *testing.T) {
	tc := setupWebsocketTestApp(t)
	defer tc.Ctrl.Finish()

	tc.ConsoleService.EXPECT().GetConsoles().Return(map[string]*domain.Console{})

	req := httptest.NewRequest("GET", "/api/v1/consoles", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.ConsolesResponse
	json.Unmarshal(body, &result)

	if len(result.Consoles) != 0 {
		t.Errorf("Expected 0 consoles, got %d", len(result.Consoles))
	}
}

func TestWebsocketHandler_Consoles_Nil(t *testing.T) {
	tc := setupWebsocketTestApp(t)
	defer tc.Ctrl.Finish()

	tc.ConsoleService.EXPECT().GetConsoles().Return(nil)

	req := httptest.NewRequest("GET", "/api/v1/consoles", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestWebsocketHandler_Consoles_SingleConsole(t *testing.T) {
	tc := setupWebsocketTestApp(t)
	defer tc.Ctrl.Finish()

	expectedConsoles := map[string]*domain.Console{
		"main.0": {
			InputMode: "stdin",
		},
	}
	tc.ConsoleService.EXPECT().GetConsoles().Return(expectedConsoles)

	req := httptest.NewRequest("GET", "/api/v1/consoles", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.ConsolesResponse
	json.Unmarshal(body, &result)

	if len(result.Consoles) != 1 {
		t.Errorf("Expected 1 console, got %d", len(result.Consoles))
	}
	if result.Consoles["main.0"].InputMode != "stdin" {
		t.Errorf("Expected input mode 'stdin', got '%s'", result.Consoles["main.0"].InputMode)
	}
}
