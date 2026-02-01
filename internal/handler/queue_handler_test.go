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

// QueueTestContext holds all mocked services for queue handler testing
type QueueTestContext struct {
	App          *fiber.App
	Ctrl         *gomock.Controller
	QueueManager *mock_ports.MockQueueManagerInterface
	Handler      *ScrollHandler // Note: Queue uses ScrollHandler struct
}

// setupQueueTestApp creates a Fiber app with mocked dependencies for testing
func setupQueueTestApp(t *testing.T) *QueueTestContext {
	ctrl := gomock.NewController(t)

	queueManager := mock_ports.NewMockQueueManagerInterface(ctrl)
	// NewQueueHandler returns *ScrollHandler
	handler := NewQueueHandler(queueManager)

	app := fiber.New()
	app.Get("/api/v1/queue", handler.Queue)

	return &QueueTestContext{
		App:          app,
		Ctrl:         ctrl,
		QueueManager: queueManager,
		Handler:      handler,
	}
}

func TestQueueHandler_Queue_Success(t *testing.T) {
	tc := setupQueueTestApp(t)
	defer tc.Ctrl.Finish()

	expectedQueue := map[string]domain.ScrollLockStatus{
		"install": "done",
		"start":   "running",
		"backup":  "waiting",
	}
	tc.QueueManager.EXPECT().GetQueue().Return(expectedQueue)

	req := httptest.NewRequest("GET", "/api/v1/queue", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]domain.ScrollLockStatus
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("Expected 3 queue items, got %d", len(result))
	}
	if result["install"] != "done" {
		t.Errorf("Expected install status 'done', got '%s'", result["install"])
	}
	if result["start"] != "running" {
		t.Errorf("Expected start status 'running', got '%s'", result["start"])
	}
}

func TestQueueHandler_Queue_Empty(t *testing.T) {
	tc := setupQueueTestApp(t)
	defer tc.Ctrl.Finish()

	tc.QueueManager.EXPECT().GetQueue().Return(map[string]domain.ScrollLockStatus{})

	req := httptest.NewRequest("GET", "/api/v1/queue", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]domain.ScrollLockStatus
	json.Unmarshal(body, &result)

	if len(result) != 0 {
		t.Errorf("Expected 0 queue items, got %d", len(result))
	}
}

func TestQueueHandler_Queue_Nil(t *testing.T) {
	tc := setupQueueTestApp(t)
	defer tc.Ctrl.Finish()

	tc.QueueManager.EXPECT().GetQueue().Return(nil)

	req := httptest.NewRequest("GET", "/api/v1/queue", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestQueueHandler_Queue_SingleItem(t *testing.T) {
	tc := setupQueueTestApp(t)
	defer tc.Ctrl.Finish()

	expectedQueue := map[string]domain.ScrollLockStatus{
		"init": "done",
	}
	tc.QueueManager.EXPECT().GetQueue().Return(expectedQueue)

	req := httptest.NewRequest("GET", "/api/v1/queue", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]domain.ScrollLockStatus
	json.Unmarshal(body, &result)

	if len(result) != 1 {
		t.Errorf("Expected 1 queue item, got %d", len(result))
	}
}
