package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

// PortTestContext holds all mocked services for port handler testing
type PortTestContext struct {
	App         *fiber.App
	Ctrl        *gomock.Controller
	PortService *mock_ports.MockPortServiceInterface
	Handler     *PortHandler
}

// setupPortTestApp creates a Fiber app with mocked dependencies for testing
func setupPortTestApp(t *testing.T) *PortTestContext {
	ctrl := gomock.NewController(t)

	portService := mock_ports.NewMockPortServiceInterface(ctrl)
	handler := NewPortHandler(portService)

	app := fiber.New()
	app.Get("/api/v1/ports", handler.GetPorts)
	app.Post("/api/v1/ports", handler.AddPort)
	app.Delete("/api/v1/ports/:port", func(c *fiber.Ctx) error {
		port, err := c.ParamsInt("port")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(api.ErrorResponse{
				Status: "error",
				Error:  "invalid port number",
			})
		}
		return handler.DeletePort(c, port)
	})

	return &PortTestContext{
		App:         app,
		Ctrl:        ctrl,
		PortService: portService,
		Handler:     handler,
	}
}

func TestPortHandler_GetPorts_Success(t *testing.T) {
	tc := setupPortTestApp(t)
	defer tc.Ctrl.Finish()

	expectedPorts := []*domain.AugmentedPort{
		{
			Port: domain.Port{
				Port:     8080,
				Protocol: "tcp",
				Name:     "http",
			},
			Open:             true,
			InactiveSince:    time.Now(),
			InactiveSinceSec: 0,
		},
		{
			Port: domain.Port{
				Port:     443,
				Protocol: "tcp",
				Name:     "https",
			},
			Open:             true,
			InactiveSinceSec: 10,
		},
	}
	tc.PortService.EXPECT().GetPorts().Return(expectedPorts)

	req := httptest.NewRequest("GET", "/api/v1/ports", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result []*domain.AugmentedPort
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 ports, got %d", len(result))
	}
	if result[0].Port.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", result[0].Port.Port)
	}
	if result[1].Port.Port != 443 {
		t.Errorf("Expected port 443, got %d", result[1].Port.Port)
	}
}

func TestPortHandler_GetPorts_Empty(t *testing.T) {
	tc := setupPortTestApp(t)
	defer tc.Ctrl.Finish()

	tc.PortService.EXPECT().GetPorts().Return([]*domain.AugmentedPort{})

	req := httptest.NewRequest("GET", "/api/v1/ports", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result []*domain.AugmentedPort
	json.Unmarshal(body, &result)

	if len(result) != 0 {
		t.Errorf("Expected 0 ports, got %d", len(result))
	}
}

func TestPortHandler_GetPorts_Nil(t *testing.T) {
	tc := setupPortTestApp(t)
	defer tc.Ctrl.Finish()

	tc.PortService.EXPECT().GetPorts().Return(nil)

	req := httptest.NewRequest("GET", "/api/v1/ports", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestPortHandler_GetPorts_WithMandatoryPort(t *testing.T) {
	tc := setupPortTestApp(t)
	defer tc.Ctrl.Finish()

	expectedPorts := []*domain.AugmentedPort{
		{
			Port: domain.Port{
				Port:      25565,
				Protocol:  "tcp",
				Name:      "minecraft",
				Mandatory: true,
			},
			Open:             false,
			InactiveSinceSec: 120,
		},
	}
	tc.PortService.EXPECT().GetPorts().Return(expectedPorts)

	req := httptest.NewRequest("GET", "/api/v1/ports", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result []*domain.AugmentedPort
	json.Unmarshal(body, &result)

	if len(result) != 1 {
		t.Errorf("Expected 1 port, got %d", len(result))
	}
	if !result[0].Mandatory {
		t.Error("Expected port to be mandatory")
	}
}

func TestPortHandler_AddPort_Success(t *testing.T) {
	tc := setupPortTestApp(t)
	defer tc.Ctrl.Finish()

	expectedPort := &domain.AugmentedPort{
		Port: domain.Port{
			Port:     8080,
			Protocol: "tcp",
			Name:     "http",
		},
		InactiveSince: time.Now(),
	}

	tc.PortService.EXPECT().AddPort(domain.Port{
		Port:     8080,
		Protocol: "tcp",
		Name:     "http",
	}).Return(expectedPort, nil)

	body := `{"port": 8080, "protocol": "tcp", "name": "http"}`
	req := httptest.NewRequest("POST", "/api/v1/ports", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Errorf("Expected status 201, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result domain.AugmentedPort
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if result.Port.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", result.Port.Port)
	}
	if result.Port.Protocol != "tcp" {
		t.Errorf("Expected protocol tcp, got %s", result.Port.Protocol)
	}
	if result.Port.Name != "http" {
		t.Errorf("Expected name http, got %s", result.Port.Name)
	}
}

func TestPortHandler_AddPort_WithOptionalFields(t *testing.T) {
	tc := setupPortTestApp(t)
	defer tc.Ctrl.Finish()

	expectedPort := &domain.AugmentedPort{
		Port: domain.Port{
			Port:          9090,
			Protocol:      "udp",
			Name:          "game",
			Mandatory:     true,
			CheckActivity: true,
			Description:   "Game server port",
		},
		InactiveSince: time.Now(),
	}

	tc.PortService.EXPECT().AddPort(domain.Port{
		Port:          9090,
		Protocol:      "udp",
		Name:          "game",
		Mandatory:     true,
		CheckActivity: true,
		Description:   "Game server port",
	}).Return(expectedPort, nil)

	body := `{"port": 9090, "protocol": "udp", "name": "game", "mandatory": true, "check_activity": true, "description": "Game server port"}`
	req := httptest.NewRequest("POST", "/api/v1/ports", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Errorf("Expected status 201, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result domain.AugmentedPort
	json.Unmarshal(respBody, &result)

	if !result.Mandatory {
		t.Error("Expected port to be mandatory")
	}
	if !result.CheckActivity {
		t.Error("Expected check_activity to be true")
	}
	if result.Description != "Game server port" {
		t.Errorf("Expected description 'Game server port', got '%s'", result.Description)
	}
}

func TestPortHandler_AddPort_InvalidBody(t *testing.T) {
	tc := setupPortTestApp(t)
	defer tc.Ctrl.Finish()

	req := httptest.NewRequest("POST", "/api/v1/ports", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result api.ErrorResponse
	json.Unmarshal(respBody, &result)

	if result.Status != "error" {
		t.Errorf("Expected status 'error', got '%s'", result.Status)
	}
}

func TestPortHandler_AddPort_DuplicatePort(t *testing.T) {
	tc := setupPortTestApp(t)
	defer tc.Ctrl.Finish()

	tc.PortService.EXPECT().AddPort(gomock.Any()).Return(nil, fmt.Errorf("port 8080 is already being watched"))

	body := `{"port": 8080, "protocol": "tcp", "name": "http"}`
	req := httptest.NewRequest("POST", "/api/v1/ports", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result api.ErrorResponse
	json.Unmarshal(respBody, &result)

	if result.Error != "port 8080 is already being watched" {
		t.Errorf("Expected duplicate port error, got '%s'", result.Error)
	}
}

func TestPortHandler_AddPort_ValidationError(t *testing.T) {
	tc := setupPortTestApp(t)
	defer tc.Ctrl.Finish()

	tc.PortService.EXPECT().AddPort(gomock.Any()).Return(nil, fmt.Errorf("port number must be between 1 and 65535, got 0"))

	body := `{"port": 0, "protocol": "tcp", "name": "invalid"}`
	req := httptest.NewRequest("POST", "/api/v1/ports", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestPortHandler_DeletePort_Success(t *testing.T) {
	tc := setupPortTestApp(t)
	defer tc.Ctrl.Finish()

	tc.PortService.EXPECT().RemovePort(8080).Return(nil)

	req := httptest.NewRequest("DELETE", "/api/v1/ports/8080", nil)

	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		t.Errorf("Expected status 204, got %d", resp.StatusCode)
	}
}

func TestPortHandler_DeletePort_NotFound(t *testing.T) {
	tc := setupPortTestApp(t)
	defer tc.Ctrl.Finish()

	tc.PortService.EXPECT().RemovePort(9999).Return(fmt.Errorf("port 9999 not found"))

	req := httptest.NewRequest("DELETE", "/api/v1/ports/9999", nil)

	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result api.ErrorResponse
	json.Unmarshal(respBody, &result)

	if result.Error != "port 9999 not found" {
		t.Errorf("Expected 'port 9999 not found', got '%s'", result.Error)
	}
}

func TestPortHandler_DeletePort_InvalidPortParam(t *testing.T) {
	tc := setupPortTestApp(t)
	defer tc.Ctrl.Finish()

	req := httptest.NewRequest("DELETE", "/api/v1/ports/notanumber", nil)

	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}
