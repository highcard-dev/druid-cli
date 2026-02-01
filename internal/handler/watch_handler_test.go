package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

// WatchTestContext holds all mocked services for watch handler testing
type WatchTestContext struct {
	App           *fiber.App
	Ctrl          *gomock.Controller
	WatchService  *mock_ports.MockWatchServiceInterface
	ScrollService *mock_ports.MockScrollServiceInterface
	Handler       *WatchHandler
}

// setupWatchTestApp creates a Fiber app with mocked dependencies for testing
func setupWatchTestApp(t *testing.T) *WatchTestContext {
	ctrl := gomock.NewController(t)

	watchService := mock_ports.NewMockWatchServiceInterface(ctrl)
	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)

	handler := NewWatchHandler(watchService, scrollService)

	app := fiber.New()
	app.Post("/api/v1/watch/enable", handler.EnableWatch)
	app.Post("/api/v1/watch/disable", handler.DisableWatch)
	app.Get("/api/v1/watch/status", handler.GetWatchStatus)

	return &WatchTestContext{
		App:           app,
		Ctrl:          ctrl,
		WatchService:  watchService,
		ScrollService: scrollService,
		Handler:       handler,
	}
}

// ============================================================================
// POST /api/v1/watch/enable Tests
// ============================================================================

func TestWatchHandler_Enable_Success(t *testing.T) {
	tc := setupWatchTestApp(t)
	defer tc.Ctrl.Finish()

	tc.WatchService.EXPECT().IsWatching().Return(false)
	tc.ScrollService.EXPECT().GetDir().Return("/path/to/scroll")
	// When request has no body or empty body, BodyParser fails, so SetHotReloadCommands is NOT called
	tc.WatchService.EXPECT().StartWatching("/path/to/scroll", gomock.Any(), gomock.Any()).Return(nil)
	tc.WatchService.EXPECT().IsWatching().Return(true)

	req := httptest.NewRequest("POST", "/api/v1/watch/enable", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d, body: %s", resp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.WatchModeResponse
	json.Unmarshal(body, &result)

	if result.Status != "success" {
		t.Errorf("Expected status 'success', got '%s'", result.Status)
	}
	if !result.Enabled {
		t.Error("Expected enabled to be true")
	}
}

func TestWatchHandler_Enable_AlreadyActive(t *testing.T) {
	tc := setupWatchTestApp(t)
	defer tc.Ctrl.Finish()

	tc.WatchService.EXPECT().IsWatching().Return(true)

	req := httptest.NewRequest("POST", "/api/v1/watch/enable", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 412 {
		t.Errorf("Expected status 412, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.WatchModeResponse
	json.Unmarshal(body, &result)

	if result.Status != "already-active" {
		t.Errorf("Expected status 'already-active', got '%s'", result.Status)
	}
}

func TestWatchHandler_Enable_NoScrollLoaded(t *testing.T) {
	tc := setupWatchTestApp(t)
	defer tc.Ctrl.Finish()

	tc.WatchService.EXPECT().IsWatching().Return(false)
	tc.ScrollService.EXPECT().GetDir().Return("")

	req := httptest.NewRequest("POST", "/api/v1/watch/enable", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.ErrorResponse
	json.Unmarshal(body, &result)

	if result.Status != "error" {
		t.Errorf("Expected status 'error', got '%s'", result.Status)
	}
}

func TestWatchHandler_Enable_StartWatchingError(t *testing.T) {
	tc := setupWatchTestApp(t)
	defer tc.Ctrl.Finish()

	tc.WatchService.EXPECT().IsWatching().Return(false)
	tc.ScrollService.EXPECT().GetDir().Return("/path/to/scroll")
	// When request has no body, BodyParser fails, so SetHotReloadCommands is NOT called
	tc.WatchService.EXPECT().StartWatching("/path/to/scroll", gomock.Any(), gomock.Any()).Return(fiber.NewError(500, "watcher error"))

	req := httptest.NewRequest("POST", "/api/v1/watch/enable", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 500 {
		t.Errorf("Expected status 500, got %d", resp.StatusCode)
	}
}

func TestWatchHandler_Enable_WithCommands(t *testing.T) {
	tc := setupWatchTestApp(t)
	defer tc.Ctrl.Finish()

	tc.WatchService.EXPECT().IsWatching().Return(false)
	tc.ScrollService.EXPECT().GetDir().Return("/path/to/scroll")
	tc.WatchService.EXPECT().SetHotReloadCommands([]string{"npm run dev"})
	tc.WatchService.EXPECT().StartWatching("/path/to/scroll", gomock.Any(), gomock.Any()).Return(nil)
	tc.WatchService.EXPECT().IsWatching().Return(true)

	hotReloadCmds := []string{"npm run dev"}
	requestBody := api.WatchModeRequest{
		HotReloadCommands: &hotReloadCmds,
	}
	bodyBytes, _ := json.Marshal(requestBody)

	req := httptest.NewRequest("POST", "/api/v1/watch/enable", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// ============================================================================
// POST /api/v1/watch/disable Tests
// ============================================================================

func TestWatchHandler_Disable_Success(t *testing.T) {
	tc := setupWatchTestApp(t)
	defer tc.Ctrl.Finish()

	tc.WatchService.EXPECT().IsWatching().Return(true)
	tc.WatchService.EXPECT().StopWatching().Return(nil)
	tc.WatchService.EXPECT().IsWatching().Return(false)

	req := httptest.NewRequest("POST", "/api/v1/watch/disable", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.WatchModeResponse
	json.Unmarshal(body, &result)

	if result.Status != "success" {
		t.Errorf("Expected status 'success', got '%s'", result.Status)
	}
	if result.Enabled {
		t.Error("Expected enabled to be false")
	}
}

func TestWatchHandler_Disable_NotWatching(t *testing.T) {
	tc := setupWatchTestApp(t)
	defer tc.Ctrl.Finish()

	tc.WatchService.EXPECT().IsWatching().Return(false)

	req := httptest.NewRequest("POST", "/api/v1/watch/disable", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.WatchModeResponse
	json.Unmarshal(body, &result)

	if result.Status != "success" {
		t.Errorf("Expected status 'success', got '%s'", result.Status)
	}
}

func TestWatchHandler_Disable_StopWatchingError(t *testing.T) {
	tc := setupWatchTestApp(t)
	defer tc.Ctrl.Finish()

	tc.WatchService.EXPECT().IsWatching().Return(true)
	tc.WatchService.EXPECT().StopWatching().Return(fiber.NewError(500, "stop error"))

	req := httptest.NewRequest("POST", "/api/v1/watch/disable", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 500 {
		t.Errorf("Expected status 500, got %d", resp.StatusCode)
	}
}

// ============================================================================
// GET /api/v1/watch/status Tests
// ============================================================================

func TestWatchHandler_Status_Enabled(t *testing.T) {
	tc := setupWatchTestApp(t)
	defer tc.Ctrl.Finish()

	watchedPaths := []string{"/path/to/public/src", "/path/to/private/src"}
	tc.WatchService.EXPECT().IsWatching().Return(true)
	tc.WatchService.EXPECT().GetWatchedPaths().Return(watchedPaths)

	req := httptest.NewRequest("GET", "/api/v1/watch/status", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.WatchStatusResponse
	json.Unmarshal(body, &result)

	if !result.Enabled {
		t.Error("Expected enabled to be true")
	}
	if len(result.WatchedPaths) != 2 {
		t.Errorf("Expected 2 watched paths, got %d", len(result.WatchedPaths))
	}
}

func TestWatchHandler_Status_Disabled(t *testing.T) {
	tc := setupWatchTestApp(t)
	defer tc.Ctrl.Finish()

	tc.WatchService.EXPECT().IsWatching().Return(false)
	tc.WatchService.EXPECT().GetWatchedPaths().Return([]string{})

	req := httptest.NewRequest("GET", "/api/v1/watch/status", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.WatchStatusResponse
	json.Unmarshal(body, &result)

	if result.Enabled {
		t.Error("Expected enabled to be false")
	}
	if len(result.WatchedPaths) != 0 {
		t.Errorf("Expected 0 watched paths, got %d", len(result.WatchedPaths))
	}
}

func TestWatchHandler_Status_NilPaths(t *testing.T) {
	tc := setupWatchTestApp(t)
	defer tc.Ctrl.Finish()

	tc.WatchService.EXPECT().IsWatching().Return(true)
	tc.WatchService.EXPECT().GetWatchedPaths().Return(nil)

	req := httptest.NewRequest("GET", "/api/v1/watch/status", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}
