package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

// HealthTestContext holds all mocked services for health handler testing
type HealthTestContext struct {
	App         *fiber.App
	Ctrl        *gomock.Controller
	PortService *mock_ports.MockPortServiceInterface
	Handler     *HealthHandler
}

// setupHealthTestApp creates a Fiber app with mocked dependencies for testing
func setupHealthTestApp(t *testing.T, timeoutSec uint) *HealthTestContext {
	return setupHealthTestAppWithProgress(t, timeoutSec, nil)
}

func setupHealthTestAppWithProgress(t *testing.T, timeoutSec uint, progress *domain.SnapshotProgress) *HealthTestContext {
	ctrl := gomock.NewController(t)

	portService := mock_ports.NewMockPortServiceInterface(ctrl)

	handler := NewHealthHandler(portService, timeoutSec, progress)

	app := fiber.New()
	app.Get("/api/v1/health", handler.GetHealthAuth)

	return &HealthTestContext{
		App:         app,
		Ctrl:        ctrl,
		PortService: portService,
		Handler:     handler,
	}
}

func TestHealthHandler_Health_MandatoryPortsNotOpen(t *testing.T) {
	tc := setupHealthTestApp(t, 0) // No timeout
	defer tc.Ctrl.Finish()

	// Ports not open and timeout not done
	tc.PortService.EXPECT().MandatoryPortsOpen().Return(false)

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 503 {
		t.Errorf("Expected status 503, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.HealthResponse
	json.Unmarshal(body, &result)

	if result.Mode != "manditory_ports" {
		t.Errorf("Expected mode 'manditory_ports', got '%s'", result.Mode)
	}
}

func TestHealthHandler_Health_Idle(t *testing.T) {
	tc := setupHealthTestApp(t, 0)
	defer tc.Ctrl.Finish()

	// Ports open, but Started is nil
	tc.PortService.EXPECT().MandatoryPortsOpen().Return(true)

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.HealthResponse
	json.Unmarshal(body, &result)

	if result.Mode != "idle" {
		t.Errorf("Expected mode 'idle', got '%s'", result.Mode)
	}
}

func TestHealthHandler_Health_Ok(t *testing.T) {
	tc := setupHealthTestApp(t, 0)
	defer tc.Ctrl.Finish()

	now := time.Now()
	tc.Handler.Started = &now

	tc.PortService.EXPECT().MandatoryPortsOpen().Return(true)

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.HealthResponse
	json.Unmarshal(body, &result)

	if result.Mode != "ok" {
		t.Errorf("Expected mode 'ok', got '%s'", result.Mode)
	}
	if result.StartDate == nil {
		t.Error("Expected StartDate to be set")
	}
}

func TestHealthHandler_Health_TimeoutDone_PortsClosed(t *testing.T) {
	tc := setupHealthTestApp(t, 0)
	defer tc.Ctrl.Finish()

	// Manually set timeoutDone to true
	tc.Handler.timeoutDone = true

	// Even with ports closed, if timeout is done, we proceed
	tc.PortService.EXPECT().MandatoryPortsOpen().Return(false)

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Should return idle since Started is nil
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.HealthResponse
	json.Unmarshal(body, &result)

	if result.Mode != "idle" {
		t.Errorf("Expected mode 'idle', got '%s'", result.Mode)
	}
}

func TestHealthHandler_Health_Restore(t *testing.T) {
	progress := domain.NewSnapshotProgress()
	progress.Mode.Store("restore")
	progress.Percentage.Store(42)

	tc := setupHealthTestAppWithProgress(t, 0, progress)
	defer tc.Ctrl.Finish()

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.HealthResponse
	json.Unmarshal(body, &result)

	if result.Mode != "restore" {
		t.Errorf("Expected mode 'restore', got '%s'", result.Mode)
	}
	if result.Progress == nil {
		t.Fatal("Expected Progress to be set")
	}
	if *result.Progress != 42 {
		t.Errorf("Expected progress 42, got %f", *result.Progress)
	}
}
