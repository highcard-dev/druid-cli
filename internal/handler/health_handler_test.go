package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

// HealthTestContext holds all mocked services for health handler testing
type HealthTestContext struct {
	App             *fiber.App
	Ctrl            *gomock.Controller
	PortService     *mock_ports.MockPortServiceInterface
	SnapshotService *mock_ports.MockSnapshotService
	Handler         *HealthHandler
}

// setupHealthTestApp creates a Fiber app with mocked dependencies for testing
func setupHealthTestApp(t *testing.T, timeoutSec uint) *HealthTestContext {
	ctrl := gomock.NewController(t)

	portService := mock_ports.NewMockPortServiceInterface(ctrl)
	snapshotService := mock_ports.NewMockSnapshotService(ctrl)

	handler := NewHealthHandler(portService, timeoutSec, snapshotService)

	app := fiber.New()
	app.Get("/api/v1/health", handler.Health)

	return &HealthTestContext{
		App:             app,
		Ctrl:            ctrl,
		PortService:     portService,
		SnapshotService: snapshotService,
		Handler:         handler,
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
	var result HealhResponse
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
	var result HealhResponse
	json.Unmarshal(body, &result)

	if result.Mode != "idle" {
		t.Errorf("Expected mode 'idle', got '%s'", result.Mode)
	}
}

func TestHealthHandler_Health_Snapshot(t *testing.T) {
	tc := setupHealthTestApp(t, 0)
	defer tc.Ctrl.Finish()

	// Set Started time
	now := time.Now()
	tc.Handler.Started = &now

	tc.PortService.EXPECT().MandatoryPortsOpen().Return(true)
	// GetCurrentMode is called twice: once in the condition, once for the response
	tc.SnapshotService.EXPECT().GetCurrentMode().Return(ports.SnapshotModeSnapshot).Times(2)

	// Create a mock progress tracker
	var pt ports.ProgressTracker = &mockProgressTracker{percent: 50.0}
	tc.SnapshotService.EXPECT().GetCurrentProgressTracker().Return(&pt)

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
	var result HealhResponse
	json.Unmarshal(body, &result)

	if result.Mode != "snapshot" {
		t.Errorf("Expected mode 'snapshot', got '%s'", result.Mode)
	}
	if result.Progress != 50.0 {
		t.Errorf("Expected progress 50.0, got %f", result.Progress)
	}
}

func TestHealthHandler_Health_Restore(t *testing.T) {
	tc := setupHealthTestApp(t, 0)
	defer tc.Ctrl.Finish()

	now := time.Now()
	tc.Handler.Started = &now

	tc.PortService.EXPECT().MandatoryPortsOpen().Return(true)
	// GetCurrentMode is called twice: once in the condition, once for the response
	tc.SnapshotService.EXPECT().GetCurrentMode().Return(ports.SnapshotModeRestore).Times(2)

	var pt ports.ProgressTracker = &mockProgressTracker{percent: 75.0}
	tc.SnapshotService.EXPECT().GetCurrentProgressTracker().Return(&pt)

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
	var result HealhResponse
	json.Unmarshal(body, &result)

	if result.Mode != "restore" {
		t.Errorf("Expected mode 'restore', got '%s'", result.Mode)
	}
}

func TestHealthHandler_Health_Ok(t *testing.T) {
	tc := setupHealthTestApp(t, 0)
	defer tc.Ctrl.Finish()

	now := time.Now()
	tc.Handler.Started = &now

	tc.PortService.EXPECT().MandatoryPortsOpen().Return(true)
	tc.SnapshotService.EXPECT().GetCurrentMode().Return(ports.SnapshotModeNoop)

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
	var result HealhResponse
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
	var result HealhResponse
	json.Unmarshal(body, &result)

	if result.Mode != "idle" {
		t.Errorf("Expected mode 'idle', got '%s'", result.Mode)
	}
}

func TestHealthHandler_Health_SnapshotNilProgressTracker(t *testing.T) {
	tc := setupHealthTestApp(t, 0)
	defer tc.Ctrl.Finish()

	now := time.Now()
	tc.Handler.Started = &now

	tc.PortService.EXPECT().MandatoryPortsOpen().Return(true)
	// GetCurrentMode is called twice: once in the condition, once for the response
	tc.SnapshotService.EXPECT().GetCurrentMode().Return(ports.SnapshotModeSnapshot).Times(2)
	tc.SnapshotService.EXPECT().GetCurrentProgressTracker().Return(nil)

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
	var result HealhResponse
	json.Unmarshal(body, &result)

	if result.Progress != 0 {
		t.Errorf("Expected progress 0, got %f", result.Progress)
	}
}

// mockProgressTracker implements ports.ProgressTracker for testing
type mockProgressTracker struct {
	percent float64
}

func (m *mockProgressTracker) LogTrackProgress(current int64) {}
func (m *mockProgressTracker) GetPercent() float64            { return m.percent }
