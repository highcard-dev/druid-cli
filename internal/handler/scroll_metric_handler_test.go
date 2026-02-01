package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

// ScrollMetricTestContext holds all mocked services for scroll metric handler testing
type ScrollMetricTestContext struct {
	App            *fiber.App
	Ctrl           *gomock.Controller
	ScrollService  *mock_ports.MockScrollServiceInterface
	ProcessMonitor *mock_ports.MockProcessMonitorInterface
	Handler        *ScrollMetricHandler
}

// setupScrollMetricTestApp creates a Fiber app with mocked dependencies for testing
func setupScrollMetricTestApp(t *testing.T) *ScrollMetricTestContext {
	ctrl := gomock.NewController(t)

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	processMonitor := mock_ports.NewMockProcessMonitorInterface(ctrl)
	handler := NewScrollMetricHandler(scrollService, processMonitor)

	app := fiber.New()
	app.Get("/api/v1/metrics", handler.Metrics)
	app.Get("/api/v1/pstree", handler.PsTree)

	return &ScrollMetricTestContext{
		App:            app,
		Ctrl:           ctrl,
		ScrollService:  scrollService,
		ProcessMonitor: processMonitor,
		Handler:        handler,
	}
}

func TestScrollMetricHandler_Metrics_Success(t *testing.T) {
	tc := setupScrollMetricTestApp(t)
	defer tc.Ctrl.Finish()

	expectedMetrics := map[string]*domain.ProcessMonitorMetrics{
		"start": {
			Cpu:    25.5,
			Memory: 1024000,
			Pid:    1234,
		},
		"worker": {
			Cpu:    10.0,
			Memory: 512000,
			Pid:    5678,
		},
	}
	tc.ProcessMonitor.EXPECT().GetAllProcessesMetrics().Return(expectedMetrics)

	req := httptest.NewRequest("GET", "/api/v1/metrics", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]*domain.ProcessMonitorMetrics
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 metrics, got %d", len(result))
	}
	if result["start"].Cpu != 25.5 {
		t.Errorf("Expected CPU 25.5, got %f", result["start"].Cpu)
	}
}

func TestScrollMetricHandler_Metrics_Empty(t *testing.T) {
	tc := setupScrollMetricTestApp(t)
	defer tc.Ctrl.Finish()

	tc.ProcessMonitor.EXPECT().GetAllProcessesMetrics().Return(map[string]*domain.ProcessMonitorMetrics{})

	req := httptest.NewRequest("GET", "/api/v1/metrics", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestScrollMetricHandler_Metrics_Nil(t *testing.T) {
	tc := setupScrollMetricTestApp(t)
	defer tc.Ctrl.Finish()

	tc.ProcessMonitor.EXPECT().GetAllProcessesMetrics().Return(nil)

	req := httptest.NewRequest("GET", "/api/v1/metrics", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestScrollMetricHandler_PsTree_Success(t *testing.T) {
	tc := setupScrollMetricTestApp(t)
	defer tc.Ctrl.Finish()

	expectedPsTree := map[string]*domain.ProcessTreeRoot{
		"start": {
			TotalProcessCount: 5,
			TotalCpuPercent:   25.0,
		},
	}
	tc.ProcessMonitor.EXPECT().GetPsTrees().Return(expectedPsTree)

	req := httptest.NewRequest("GET", "/api/v1/pstree", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]*domain.ProcessTreeRoot
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 ps tree entry, got %d", len(result))
	}
	if result["start"].TotalProcessCount != 5 {
		t.Errorf("Expected TotalProcessCount 5, got %d", result["start"].TotalProcessCount)
	}
}

func TestScrollMetricHandler_PsTree_Empty(t *testing.T) {
	tc := setupScrollMetricTestApp(t)
	defer tc.Ctrl.Finish()

	tc.ProcessMonitor.EXPECT().GetPsTrees().Return(map[string]*domain.ProcessTreeRoot{})

	req := httptest.NewRequest("GET", "/api/v1/pstree", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestScrollMetricHandler_PsTree_Nil(t *testing.T) {
	tc := setupScrollMetricTestApp(t)
	defer tc.Ctrl.Finish()

	tc.ProcessMonitor.EXPECT().GetPsTrees().Return(nil)

	req := httptest.NewRequest("GET", "/api/v1/pstree", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestScrollMetricHandler_PsTree_MultipleProcesses(t *testing.T) {
	tc := setupScrollMetricTestApp(t)
	defer tc.Ctrl.Finish()

	expectedPsTree := map[string]*domain.ProcessTreeRoot{
		"start": {
			TotalProcessCount: 3,
			TotalCpuPercent:   15.0,
		},
		"worker": {
			TotalProcessCount: 2,
			TotalCpuPercent:   10.0,
		},
	}
	tc.ProcessMonitor.EXPECT().GetPsTrees().Return(expectedPsTree)

	req := httptest.NewRequest("GET", "/api/v1/pstree", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]*domain.ProcessTreeRoot
	json.Unmarshal(body, &result)

	if len(result) != 2 {
		t.Errorf("Expected 2 ps tree entries, got %d", len(result))
	}
}
