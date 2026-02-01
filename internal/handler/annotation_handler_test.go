package handler

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

// AnnotationTestContext holds all mocked services for annotation handler testing
type AnnotationTestContext struct {
	App           *fiber.App
	Ctrl          *gomock.Controller
	ScrollService *mock_ports.MockScrollServiceInterface
	Handler       *AnnotationHandler
}

// setupAnnotationTestApp creates a Fiber app with mocked dependencies for testing
func setupAnnotationTestApp(t *testing.T) *AnnotationTestContext {
	ctrl := gomock.NewController(t)

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	handler := NewAnnotationHandler(scrollService)

	app := fiber.New()
	app.Get("/annotations", handler.Annotations)

	return &AnnotationTestContext{
		App:           app,
		Ctrl:          ctrl,
		ScrollService: scrollService,
		Handler:       handler,
	}
}

func TestAnnotationHandler_Annotations_Success(t *testing.T) {
	tc := setupAnnotationTestApp(t)
	defer tc.Ctrl.Finish()

	// Create a temporary directory and file for testing
	tempDir, err := os.MkdirTemp("", "annotation-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create annotations.json file
	annotationsFile := filepath.Join(tempDir, "annotations.json")
	annotationsContent := `{"key": "value"}`
	if err := os.WriteFile(annotationsFile, []byte(annotationsContent), 0644); err != nil {
		t.Fatalf("Failed to create annotations file: %v", err)
	}

	tc.ScrollService.EXPECT().GetDir().Return(tempDir)

	req := httptest.NewRequest("GET", "/annotations", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestAnnotationHandler_Annotations_FileNotFound(t *testing.T) {
	tc := setupAnnotationTestApp(t)
	defer tc.Ctrl.Finish()

	// Return a directory that doesn't exist
	tc.ScrollService.EXPECT().GetDir().Return("/non/existent/path")

	req := httptest.NewRequest("GET", "/annotations", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Fiber returns 404 when file is not found
	if resp.StatusCode != 404 {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}
}

func TestAnnotationHandler_Annotations_EmptyDir(t *testing.T) {
	tc := setupAnnotationTestApp(t)
	defer tc.Ctrl.Finish()

	// Create temp dir without annotations file
	tempDir, err := os.MkdirTemp("", "annotation-test-empty")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tc.ScrollService.EXPECT().GetDir().Return(tempDir)

	req := httptest.NewRequest("GET", "/annotations", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	// Should return 404 when file doesn't exist
	if resp.StatusCode != 404 {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}
}

func TestAnnotationHandler_Annotations_ValidJSON(t *testing.T) {
	tc := setupAnnotationTestApp(t)
	defer tc.Ctrl.Finish()

	tempDir, err := os.MkdirTemp("", "annotation-test-json")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a valid JSON annotations file
	annotationsFile := filepath.Join(tempDir, "annotations.json")
	annotationsContent := `{
		"annotations": [
			{"name": "cpu", "value": "50%"},
			{"name": "memory", "value": "1GB"}
		]
	}`
	if err := os.WriteFile(annotationsFile, []byte(annotationsContent), 0644); err != nil {
		t.Fatalf("Failed to create annotations file: %v", err)
	}

	tc.ScrollService.EXPECT().GetDir().Return(tempDir)

	req := httptest.NewRequest("GET", "/annotations", nil)
	resp, err := tc.App.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		// Note: Fiber sets content type based on file extension
		t.Logf("Content-Type: %s", contentType)
	}
}
