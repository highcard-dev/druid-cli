package handler_test

import (
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/handler"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

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
