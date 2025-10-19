package handler_test

import (
	"errors"
	"io"
	"net/http/httptest"
	"os"
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

	t.Run("PublicIndex_404", func(t *testing.T) {
		mockUiService.EXPECT().
			GetIndex("public").
			Return(nil, os.ErrNotExist)

		req := httptest.NewRequest("GET", "/public/index", nil)
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
		expectedBody := "Public UI directory not found"
		if string(body) != expectedBody {
			t.Errorf("Expected body %s, got %s", expectedBody, string(body))
		}
	})

	t.Run("PublicIndex_500", func(t *testing.T) {
		errMsg := "some internal error"
		mockUiService.EXPECT().
			GetIndex("public").
			Return(nil, errors.New(errMsg))

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
		expectedBody := `{"error":"` + errMsg + `"}`
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

	t.Run("PrivateIndex_404", func(t *testing.T) {
		mockUiService.EXPECT().
			GetIndex("private").
			Return(nil, os.ErrNotExist)

		req := httptest.NewRequest("GET", "/private/index", nil)
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
		expectedBody := "Private UI directory not found"
		if string(body) != expectedBody {
			t.Errorf("Expected body %s, got %s", expectedBody, string(body))
		}
	})

	t.Run("PrivateIndex_500", func(t *testing.T) {
		errMsg := "some internal error"
		mockUiService.EXPECT().
			GetIndex("private").
			Return(nil, errors.New(errMsg))

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
		expectedBody := `{"error":"` + errMsg + `"}`
		if string(body) != expectedBody {
			t.Errorf("Expected body %s, got %s", expectedBody, string(body))
		}
	})
}
