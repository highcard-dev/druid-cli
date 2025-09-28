package handler_test

import (
	"bytes"
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/handler"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

func TestUiHandler_Public(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUiService := mock_ports.NewMockUiServiceInterface(ctrl)
	uiHandler := handler.NewUiHandler(mockUiService)

	app := fiber.New()
	app.Get("/public/*", uiHandler.Public)
	app.Get("/public/index", uiHandler.PublicIndex)

	t.Run("Public_ServeFile", func(t *testing.T) {
		expectedContent := []byte("<html><body>Test</body></html>")
		expectedContentType := "text/html; charset=utf-8"

		mockUiService.EXPECT().
			ServeFile("public", "test.html").
			Return(expectedContent, expectedContentType, nil)

		req := httptest.NewRequest("GET", "/public/test.html", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}

		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType != expectedContentType {
			t.Errorf("Expected Content-Type %s, got %s", expectedContentType, contentType)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		if !bytes.Equal(body, expectedContent) {
			t.Errorf("Expected body %s, got %s", string(expectedContent), string(body))
		}
	})

	t.Run("Public_ServeFile_NotFound", func(t *testing.T) {
		mockUiService.EXPECT().
			ServeFile("public", "notfound.html").
			Return(nil, "", errors.New("file not found"))

		req := httptest.NewRequest("GET", "/public/notfound.html", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}

		if resp.StatusCode != 404 {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		expectedBody := "File not found"
		if string(body) != expectedBody {
			t.Errorf("Expected body %s, got %s", expectedBody, string(body))
		}
	})

	t.Run("Public_EmptyPath_CallsIndex", func(t *testing.T) {
		expectedFiles := []string{"/path/to/file1.html", "/path/to/file2.css"}

		mockUiService.EXPECT().
			GetIndex("public").
			Return(expectedFiles, nil)

		req := httptest.NewRequest("GET", "/public/", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}

		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		// Should return JSON array of files
		expectedJSON := `["/path/to/file1.html","/path/to/file2.css"]`
		if string(body) != expectedJSON {
			t.Errorf("Expected body %s, got %s", expectedJSON, string(body))
		}
	})
}

func TestUiHandler_Private(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUiService := mock_ports.NewMockUiServiceInterface(ctrl)
	uiHandler := handler.NewUiHandler(mockUiService)

	app := fiber.New()
	app.Get("/private/*", uiHandler.Private)
	app.Get("/private/index", uiHandler.PrivateIndex)

	t.Run("Private_ServeFile", func(t *testing.T) {
		expectedContent := []byte("body { color: blue; }")
		expectedContentType := "text/css; charset=utf-8"

		mockUiService.EXPECT().
			ServeFile("private", "admin.css").
			Return(expectedContent, expectedContentType, nil)

		req := httptest.NewRequest("GET", "/private/admin.css", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}

		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType != expectedContentType {
			t.Errorf("Expected Content-Type %s, got %s", expectedContentType, contentType)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		if !bytes.Equal(body, expectedContent) {
			t.Errorf("Expected body %s, got %s", string(expectedContent), string(body))
		}
	})

	t.Run("Private_ServeFile_NotFound", func(t *testing.T) {
		mockUiService.EXPECT().
			ServeFile("private", "notfound.css").
			Return(nil, "", errors.New("file not found"))

		req := httptest.NewRequest("GET", "/private/notfound.css", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}

		if resp.StatusCode != 404 {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		expectedBody := "File not found"
		if string(body) != expectedBody {
			t.Errorf("Expected body %s, got %s", expectedBody, string(body))
		}
	})
}

func TestUiHandler_PublicIndex(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUiService := mock_ports.NewMockUiServiceInterface(ctrl)
	uiHandler := handler.NewUiHandler(mockUiService)

	app := fiber.New()
	app.Get("/public/index", uiHandler.PublicIndex)

	t.Run("PublicIndex_Success", func(t *testing.T) {
		expectedFiles := []string{"/scroll/public/index.html", "/scroll/public/style.css"}

		mockUiService.EXPECT().
			GetIndex("public").
			Return(expectedFiles, nil)

		req := httptest.NewRequest("GET", "/public/index", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}

		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		expectedJSON := `["/scroll/public/index.html","/scroll/public/style.css"]`
		if string(body) != expectedJSON {
			t.Errorf("Expected body %s, got %s", expectedJSON, string(body))
		}
	})

	t.Run("PublicIndex_Error", func(t *testing.T) {
		mockUiService.EXPECT().
			GetIndex("public").
			Return(nil, errors.New("directory not found"))

		req := httptest.NewRequest("GET", "/public/index", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}

		if resp.StatusCode != 500 {
			t.Errorf("Expected status 500, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		expectedBody := "Failed to retrieve public UI index"
		if string(body) != expectedBody {
			t.Errorf("Expected body %s, got %s", expectedBody, string(body))
		}
	})
}

func TestUiHandler_PrivateIndex(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUiService := mock_ports.NewMockUiServiceInterface(ctrl)
	uiHandler := handler.NewUiHandler(mockUiService)

	app := fiber.New()
	app.Get("/private/index", uiHandler.PrivateIndex)

	t.Run("PrivateIndex_Success", func(t *testing.T) {
		expectedFiles := []string{"/scroll/private/admin.html", "/scroll/private/config.js"}

		mockUiService.EXPECT().
			GetIndex("private").
			Return(expectedFiles, nil)

		req := httptest.NewRequest("GET", "/private/index", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}

		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		expectedJSON := `["/scroll/private/admin.html","/scroll/private/config.js"]`
		if string(body) != expectedJSON {
			t.Errorf("Expected body %s, got %s", expectedJSON, string(body))
		}
	})

	t.Run("PrivateIndex_Error", func(t *testing.T) {
		mockUiService.EXPECT().
			GetIndex("private").
			Return(nil, errors.New("directory not found"))

		req := httptest.NewRequest("GET", "/private/index", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}

		if resp.StatusCode != 500 {
			t.Errorf("Expected status 500, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		expectedBody := "Failed to retrieve private UI index"
		if string(body) != expectedBody {
			t.Errorf("Expected body %s, got %s", expectedBody, string(body))
		}
	})
}
