package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

// ColdstarterTestContext holds all mocked services for coldstarter handler testing
type ColdstarterTestContext struct {
	App         *fiber.App
	Ctrl        *gomock.Controller
	Coldstarter *mock_ports.MockColdStarterInterface
	Handler     *ColdstarterHandler
}

// setupColdstarterTestApp creates a Fiber app with mocked dependencies for testing
func setupColdstarterTestApp(t *testing.T) *ColdstarterTestContext {
	ctrl := gomock.NewController(t)

	coldstarter := mock_ports.NewMockColdStarterInterface(ctrl)
	handler := NewColdstarterHandler(coldstarter)

	app := fiber.New()
	app.Post("/api/v1/coldstarter/finish", handler.Finish)

	return &ColdstarterTestContext{
		App:         app,
		Ctrl:        ctrl,
		Coldstarter: coldstarter,
		Handler:     handler,
	}
}

func TestColdstarterHandler_Finish_Success(t *testing.T) {
	tc := setupColdstarterTestApp(t)
	defer tc.Ctrl.Finish()

	// Finish is called with nil argument
	tc.Coldstarter.EXPECT().Finish(nil)

	req := httptest.NewRequest("POST", "/api/v1/coldstarter/finish", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 202 {
		t.Errorf("Expected status 202, got %d", resp.StatusCode)
	}
}

func TestColdstarterHandler_Finish_CalledOnce(t *testing.T) {
	tc := setupColdstarterTestApp(t)
	defer tc.Ctrl.Finish()

	// Verify Finish is called exactly once
	tc.Coldstarter.EXPECT().Finish(nil).Times(1)

	req := httptest.NewRequest("POST", "/api/v1/coldstarter/finish", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 202 {
		t.Errorf("Expected status 202, got %d", resp.StatusCode)
	}
}

func TestColdstarterHandler_Finish_WithBody(t *testing.T) {
	tc := setupColdstarterTestApp(t)
	defer tc.Ctrl.Finish()

	// Handler ignores request body, still calls Finish with nil
	tc.Coldstarter.EXPECT().Finish(nil)

	req := httptest.NewRequest("POST", "/api/v1/coldstarter/finish", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 202 {
		t.Errorf("Expected status 202, got %d", resp.StatusCode)
	}
}
