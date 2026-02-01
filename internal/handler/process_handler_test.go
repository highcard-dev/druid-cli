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

// ProcessTestContext holds all mocked services for process handler testing
type ProcessTestContext struct {
	App            *fiber.App
	Ctrl           *gomock.Controller
	ProcessManager *mock_ports.MockProcessManagerInterface
	Handler        *ProcessHandler
}

// setupProcessTestApp creates a Fiber app with mocked dependencies for testing
func setupProcessTestApp(t *testing.T) *ProcessTestContext {
	ctrl := gomock.NewController(t)

	processManager := mock_ports.NewMockProcessManagerInterface(ctrl)
	handler := NewProcessHandler(processManager)

	app := fiber.New()
	app.Get("/api/v1/processes", handler.GetProcesses)

	return &ProcessTestContext{
		App:            app,
		Ctrl:           ctrl,
		ProcessManager: processManager,
		Handler:        handler,
	}
}

func TestProcessHandler_Processes_Success(t *testing.T) {
	tc := setupProcessTestApp(t)
	defer tc.Ctrl.Finish()

	expectedProcesses := map[string]*domain.Process{
		"start": {
			Name: "start",
			Type: "tty",
		},
		"install": {
			Name: "install",
			Type: "exec",
		},
	}
	tc.ProcessManager.EXPECT().GetRunningProcesses().Return(expectedProcesses)

	req := httptest.NewRequest("GET", "/api/v1/processes", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.ProcessesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(result.Processes) != 2 {
		t.Errorf("Expected 2 processes, got %d", len(result.Processes))
	}
	if _, ok := result.Processes["start"]; !ok {
		t.Error("Expected 'start' process to be present")
	}
	if _, ok := result.Processes["install"]; !ok {
		t.Error("Expected 'install' process to be present")
	}
}

func TestProcessHandler_Processes_Empty(t *testing.T) {
	tc := setupProcessTestApp(t)
	defer tc.Ctrl.Finish()

	tc.ProcessManager.EXPECT().GetRunningProcesses().Return(map[string]*domain.Process{})

	req := httptest.NewRequest("GET", "/api/v1/processes", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.ProcessesResponse
	json.Unmarshal(body, &result)

	if len(result.Processes) != 0 {
		t.Errorf("Expected 0 processes, got %d", len(result.Processes))
	}
}

func TestProcessHandler_Processes_Nil(t *testing.T) {
	tc := setupProcessTestApp(t)
	defer tc.Ctrl.Finish()

	tc.ProcessManager.EXPECT().GetRunningProcesses().Return(nil)

	req := httptest.NewRequest("GET", "/api/v1/processes", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestProcessHandler_Processes_SingleProcess(t *testing.T) {
	tc := setupProcessTestApp(t)
	defer tc.Ctrl.Finish()

	expectedProcesses := map[string]*domain.Process{
		"main": {
			Name: "main",
			Type: "tty",
		},
	}
	tc.ProcessManager.EXPECT().GetRunningProcesses().Return(expectedProcesses)

	req := httptest.NewRequest("GET", "/api/v1/processes", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result api.ProcessesResponse
	json.Unmarshal(body, &result)

	if len(result.Processes) != 1 {
		t.Errorf("Expected 1 process, got %d", len(result.Processes))
	}
	if result.Processes["main"].Name != "main" {
		t.Errorf("Expected name 'main', got '%s'", result.Processes["main"].Name)
	}
}
