package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
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
