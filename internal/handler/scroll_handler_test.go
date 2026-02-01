package handler

import (
	"bytes"
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

// TestContext holds all mocked services for testing
type TestContext struct {
	App               *fiber.App
	Ctrl              *gomock.Controller
	ScrollService     *mock_ports.MockScrollServiceInterface
	PluginManager     *mock_ports.MockPluginManagerInterface
	ProcedureLauncher *mock_ports.MockProcedureLauchnerInterface
	QueueManager      *mock_ports.MockQueueManagerInterface
	ProcessManager    *mock_ports.MockProcessManagerInterface
	Handler           *ScrollHandler
}

// setupTestApp creates a Fiber app with mocked dependencies for testing
func setupTestApp(t *testing.T) *TestContext {
	ctrl := gomock.NewController(t)

	// Create mocked services
	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	pluginManager := mock_ports.NewMockPluginManagerInterface(ctrl)
	procedureLauncher := mock_ports.NewMockProcedureLauchnerInterface(ctrl)
	queueManager := mock_ports.NewMockQueueManagerInterface(ctrl)
	processManager := mock_ports.NewMockProcessManagerInterface(ctrl)

	// Create handler with mocks
	handler := NewScrollHandler(scrollService, pluginManager, procedureLauncher, queueManager, processManager)

	// Create minimal Fiber app for testing
	app := fiber.New()
	app.Get("/api/v1/scroll", handler.GetScroll)
	app.Post("/api/v1/command", handler.RunCommand)
	app.Post("/api/v1/procedure", handler.RunProcedure)
	app.Get("/api/v1/procedures", handler.Procedures)

	return &TestContext{
		App:               app,
		Ctrl:              ctrl,
		ScrollService:     scrollService,
		PluginManager:     pluginManager,
		ProcedureLauncher: procedureLauncher,
		QueueManager:      queueManager,
		ProcessManager:    processManager,
		Handler:           handler,
	}
}

// ============================================================================
// GET /api/v1/scroll Tests
// ============================================================================

func TestScrollHandler_GetScroll_Success(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock expectations
	expectedFile := &domain.File{
		Name:       "test-scroll",
		Desc:       "Test scroll description",
		AppVersion: "1.0.0",
	}
	tc.ScrollService.EXPECT().GetFile().Return(expectedFile)

	// Create request
	req := httptest.NewRequest("GET", "/api/v1/scroll", nil)
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	var result domain.File
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if result.Name != expectedFile.Name {
		t.Errorf("Expected name %s, got %s", expectedFile.Name, result.Name)
	}
	if result.Desc != expectedFile.Desc {
		t.Errorf("Expected desc %s, got %s", expectedFile.Desc, result.Desc)
	}
}

func TestScrollHandler_GetScroll_NilFile(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock to return nil
	tc.ScrollService.EXPECT().GetFile().Return(nil)

	// Create request
	req := httptest.NewRequest("GET", "/api/v1/scroll", nil)

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Should still return 200 with null body
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// ============================================================================
// POST /api/v1/command Tests
// ============================================================================

func TestScrollHandler_RunCommand_SyncSuccess(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock expectations for sync command
	tc.QueueManager.EXPECT().AddTempItem("test-command").Return(nil)

	// Create request body
	requestBody := StartScrollRequestBody{
		CommandId: "test-command",
		Sync:      true,
	}
	bodyBytes, _ := json.Marshal(requestBody)

	// Create request
	req := httptest.NewRequest("POST", "/api/v1/command", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status (200 for sync)
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestScrollHandler_RunCommand_AsyncSuccess(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock expectations for async command
	tc.QueueManager.EXPECT().AddTempItem("test-command").Return(nil)

	// Create request body
	requestBody := StartScrollRequestBody{
		CommandId: "test-command",
		Sync:      false,
	}
	bodyBytes, _ := json.Marshal(requestBody)

	// Create request
	req := httptest.NewRequest("POST", "/api/v1/command", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status (201 for async)
	if resp.StatusCode != 201 {
		t.Errorf("Expected status 201, got %d", resp.StatusCode)
	}

	// Give async goroutine time to complete
	time.Sleep(100 * time.Millisecond)
}

func TestScrollHandler_RunCommand_InvalidBody(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Create request with invalid JSON
	req := httptest.NewRequest("POST", "/api/v1/command", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status (400 for invalid body)
	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestScrollHandler_RunCommand_SyncError(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock to return error
	tc.QueueManager.EXPECT().AddTempItem("test-command").Return(fiber.NewError(500, "internal error"))

	// Create request body
	requestBody := StartScrollRequestBody{
		CommandId: "test-command",
		Sync:      true,
	}
	bodyBytes, _ := json.Marshal(requestBody)

	// Create request
	req := httptest.NewRequest("POST", "/api/v1/command", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status (500 for error)
	if resp.StatusCode != 500 {
		t.Errorf("Expected status 500, got %d", resp.StatusCode)
	}
}

func TestScrollHandler_RunCommand_EmptyBody(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Create request with empty body
	req := httptest.NewRequest("POST", "/api/v1/command", nil)
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status (400 for empty body)
	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

// ============================================================================
// POST /api/v1/procedure Tests
// ============================================================================

func TestScrollHandler_RunProcedure_SyncSuccess(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock expectations
	tc.PluginManager.EXPECT().CanRunStandaloneProcedure("rcon").Return(true)
	tc.ProcessManager.EXPECT().GetRunningProcess("test-process").Return(&domain.Process{})
	tc.ProcedureLauncher.EXPECT().RunProcedure(gomock.Any(), "test-process", []string{"dep1"}).Return("result", nil, nil)

	// Create request body
	requestBody := StartProcedureRequestBody{
		Mode:         "rcon",
		Data:         "test-data",
		Process:      "test-process",
		Dependencies: []string{"dep1"},
		Sync:         true,
	}
	bodyBytes, _ := json.Marshal(requestBody)

	// Create request
	req := httptest.NewRequest("POST", "/api/v1/procedure", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status (200 for sync success)
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d, body: %s", resp.StatusCode, string(body))
	}
}

func TestScrollHandler_RunProcedure_AsyncSuccess(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock expectations
	tc.PluginManager.EXPECT().CanRunStandaloneProcedure("rcon").Return(true)
	tc.ProcessManager.EXPECT().GetRunningProcess("test-process").Return(&domain.Process{})
	tc.ProcedureLauncher.EXPECT().RunProcedure(gomock.Any(), "test-process", []string{}).Return("", nil, nil)

	// Create request body
	requestBody := StartProcedureRequestBody{
		Mode:         "rcon",
		Data:         "test-data",
		Process:      "test-process",
		Dependencies: []string{},
		Sync:         false,
	}
	bodyBytes, _ := json.Marshal(requestBody)

	// Create request
	req := httptest.NewRequest("POST", "/api/v1/procedure", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status (201 for async)
	if resp.StatusCode != 201 {
		t.Errorf("Expected status 201, got %d", resp.StatusCode)
	}

	// Give async goroutine time to complete
	time.Sleep(100 * time.Millisecond)
}

func TestScrollHandler_RunProcedure_StdinMode(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock expectations - stdin mode is always allowed
	// Note: CanRunStandaloneProcedure is still called due to evaluation order, but the condition short-circuits
	tc.PluginManager.EXPECT().CanRunStandaloneProcedure("stdin").Return(false)
	tc.ProcessManager.EXPECT().GetRunningProcess("test-process").Return(&domain.Process{})
	tc.ProcedureLauncher.EXPECT().RunProcedure(gomock.Any(), "test-process", []string{}).Return("result", nil, nil)

	// Create request body with stdin mode
	requestBody := StartProcedureRequestBody{
		Mode:         "stdin",
		Data:         "test-data",
		Process:      "test-process",
		Dependencies: []string{},
		Sync:         true,
	}
	bodyBytes, _ := json.Marshal(requestBody)

	// Create request
	req := httptest.NewRequest("POST", "/api/v1/procedure", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d, body: %s", resp.StatusCode, string(body))
	}
}

func TestScrollHandler_RunProcedure_InvalidMode(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock - mode not allowed
	tc.PluginManager.EXPECT().CanRunStandaloneProcedure("invalid-mode").Return(false)

	// Create request body
	requestBody := StartProcedureRequestBody{
		Mode:         "invalid-mode",
		Data:         "test-data",
		Process:      "test-process",
		Dependencies: []string{},
		Sync:         true,
	}
	bodyBytes, _ := json.Marshal(requestBody)

	// Create request
	req := httptest.NewRequest("POST", "/api/v1/procedure", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status (400 for invalid mode)
	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestScrollHandler_RunProcedure_EmptyData(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock
	tc.PluginManager.EXPECT().CanRunStandaloneProcedure("rcon").Return(true)

	// Create request body with empty data
	requestBody := StartProcedureRequestBody{
		Mode:         "rcon",
		Data:         "",
		Process:      "test-process",
		Dependencies: []string{},
		Sync:         true,
	}
	bodyBytes, _ := json.Marshal(requestBody)

	// Create request
	req := httptest.NewRequest("POST", "/api/v1/procedure", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status (400 for empty data)
	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestScrollHandler_RunProcedure_ProcessNotFound(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock - process not found
	tc.PluginManager.EXPECT().CanRunStandaloneProcedure("rcon").Return(true)
	tc.ProcessManager.EXPECT().GetRunningProcess("non-existent").Return(nil)

	// Create request body
	requestBody := StartProcedureRequestBody{
		Mode:         "rcon",
		Data:         "test-data",
		Process:      "non-existent",
		Dependencies: []string{},
		Sync:         true,
	}
	bodyBytes, _ := json.Marshal(requestBody)

	// Create request
	req := httptest.NewRequest("POST", "/api/v1/procedure", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status (400 for process not found)
	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestScrollHandler_RunProcedure_InvalidBody(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Create request with invalid JSON
	req := httptest.NewRequest("POST", "/api/v1/procedure", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status (400 for invalid body)
	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestScrollHandler_RunProcedure_SyncError(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock expectations - procedure returns error
	tc.PluginManager.EXPECT().CanRunStandaloneProcedure("rcon").Return(true)
	tc.ProcessManager.EXPECT().GetRunningProcess("test-process").Return(&domain.Process{})
	tc.ProcedureLauncher.EXPECT().RunProcedure(gomock.Any(), "test-process", []string{}).Return("", nil, fiber.NewError(500, "procedure failed"))

	// Create request body
	requestBody := StartProcedureRequestBody{
		Mode:         "rcon",
		Data:         "test-data",
		Process:      "test-process",
		Dependencies: []string{},
		Sync:         true,
	}
	bodyBytes, _ := json.Marshal(requestBody)

	// Create request
	req := httptest.NewRequest("POST", "/api/v1/procedure", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status (400 for procedure error)
	if resp.StatusCode != 400 {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

// ============================================================================
// GET /api/v1/procedures Tests
// ============================================================================

func TestScrollHandler_Procedures_Success(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock expectations
	expectedStatuses := map[string]domain.ScrollLockStatus{
		"install": "done",
		"start":   "running",
	}
	tc.ProcedureLauncher.EXPECT().GetProcedureStatuses().Return(expectedStatuses)

	// Create request
	req := httptest.NewRequest("GET", "/api/v1/procedures", nil)

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	var result map[string]domain.ScrollLockStatus
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(result) != len(expectedStatuses) {
		t.Errorf("Expected %d statuses, got %d", len(expectedStatuses), len(result))
	}

	for key, expectedValue := range expectedStatuses {
		if result[key] != expectedValue {
			t.Errorf("Expected status %s for %s, got %s", expectedValue, key, result[key])
		}
	}
}

func TestScrollHandler_Procedures_Empty(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock to return empty map
	tc.ProcedureLauncher.EXPECT().GetProcedureStatuses().Return(map[string]domain.ScrollLockStatus{})

	// Create request
	req := httptest.NewRequest("GET", "/api/v1/procedures", nil)

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Verify response status
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify empty response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	var result map[string]domain.ScrollLockStatus
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected 0 statuses, got %d", len(result))
	}
}

func TestScrollHandler_Procedures_NilMap(t *testing.T) {
	tc := setupTestApp(t)
	defer tc.Ctrl.Finish()

	// Setup mock to return nil
	tc.ProcedureLauncher.EXPECT().GetProcedureStatuses().Return(nil)

	// Create request
	req := httptest.NewRequest("GET", "/api/v1/procedures", nil)

	// Execute request
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Should still return 200
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}
