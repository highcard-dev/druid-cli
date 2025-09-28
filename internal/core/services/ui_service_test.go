package services_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/services"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

func TestUiService_GetIndex(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockScrollService := mock_ports.NewMockScrollServiceInterface(ctrl)

	// Create a temporary directory structure for testing
	tempDir := t.TempDir()
	publicDir := filepath.Join(tempDir, "public")
	err := os.MkdirAll(publicDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Create test files
	testFiles := []string{"index.html", "style.css", "script.js"}
	for _, file := range testFiles {
		filePath := filepath.Join(publicDir, file)
		err := os.WriteFile(filePath, []byte("test content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file %s: %v", file, err)
		}
	}

	// Create a subdirectory with a file
	subDir := filepath.Join(publicDir, "assets")
	err = os.MkdirAll(subDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}
	err = os.WriteFile(filepath.Join(subDir, "image.png"), []byte("image data"), 0644)
	if err != nil {
		t.Fatalf("Failed to create subdirectory file: %v", err)
	}

	mockScrollService.EXPECT().GetDir().Return(tempDir).AnyTimes()

	uiService := services.NewUiService(mockScrollService)

	t.Run("GetIndex_Success", func(t *testing.T) {
		files, err := uiService.GetIndex("public")
		if err != nil {
			t.Errorf("GetIndex returned error: %v", err)
		}

		if len(files) != 4 { // 3 files + 1 in subdirectory
			t.Errorf("Expected 4 files, got %d", len(files))
		}

		// Verify all expected files are present
		expectedFiles := []string{
			filepath.Join(publicDir, "index.html"),
			filepath.Join(publicDir, "style.css"),
			filepath.Join(publicDir, "script.js"),
			filepath.Join(publicDir, "assets", "image.png"),
		}

		for _, expected := range expectedFiles {
			found := false
			for _, actual := range files {
				if actual == expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected file %s not found in result", expected)
			}
		}
	})

	t.Run("GetIndex_NonExistentDirectory", func(t *testing.T) {
		files, err := uiService.GetIndex("nonexistent")
		if err == nil {
			t.Error("Expected error for non-existent directory")
		}
		if len(files) != 0 {
			t.Error("Expected empty files list for non-existent directory")
		}
	})
}

func TestUiService_ServeFile(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockScrollService := mock_ports.NewMockScrollServiceInterface(ctrl)

	// Create a temporary directory structure for testing
	tempDir := t.TempDir()
	publicDir := filepath.Join(tempDir, "public")
	err := os.MkdirAll(publicDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Create test files with different content types
	testContent := "<!DOCTYPE html><html><body>Hello World</body></html>"
	htmlFile := filepath.Join(publicDir, "index.html")
	err = os.WriteFile(htmlFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create HTML test file: %v", err)
	}

	cssContent := "body { color: red; }"
	cssFile := filepath.Join(publicDir, "style.css")
	err = os.WriteFile(cssFile, []byte(cssContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create CSS test file: %v", err)
	}

	// Create a file with no extension
	noExtFile := filepath.Join(publicDir, "README")
	err = os.WriteFile(noExtFile, []byte("readme content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create no extension test file: %v", err)
	}

	mockScrollService.EXPECT().GetDir().Return(tempDir).AnyTimes()

	uiService := services.NewUiService(mockScrollService)

	t.Run("ServeFile_HTMLFile", func(t *testing.T) {
		content, contentType, err := uiService.ServeFile("public", "index.html")
		if err != nil {
			t.Errorf("ServeFile returned error: %v", err)
		}

		if string(content) != testContent {
			t.Errorf("Expected content %s, got %s", testContent, string(content))
		}

		expectedContentType := "text/html; charset=utf-8"
		if contentType != expectedContentType {
			t.Errorf("Expected content type %s, got %s", expectedContentType, contentType)
		}
	})

	t.Run("ServeFile_CSSFile", func(t *testing.T) {
		content, contentType, err := uiService.ServeFile("public", "style.css")
		if err != nil {
			t.Errorf("ServeFile returned error: %v", err)
		}

		if string(content) != cssContent {
			t.Errorf("Expected content %s, got %s", cssContent, string(content))
		}

		expectedContentType := "text/css; charset=utf-8"
		if contentType != expectedContentType {
			t.Errorf("Expected content type %s, got %s", expectedContentType, contentType)
		}
	})

	t.Run("ServeFile_NoExtension", func(t *testing.T) {
		content, contentType, err := uiService.ServeFile("public", "README")
		if err != nil {
			t.Errorf("ServeFile returned error: %v", err)
		}

		if string(content) != "readme content" {
			t.Errorf("Expected content 'readme content', got %s", string(content))
		}

		expectedContentType := "application/octet-stream"
		if contentType != expectedContentType {
			t.Errorf("Expected content type %s, got %s", expectedContentType, contentType)
		}
	})

	t.Run("ServeFile_NonExistentFile", func(t *testing.T) {
		content, contentType, err := uiService.ServeFile("public", "nonexistent.html")
		if err == nil {
			t.Error("Expected error for non-existent file")
		}
		if content != nil {
			t.Error("Expected nil content for non-existent file")
		}
		if contentType != "" {
			t.Error("Expected empty content type for non-existent file")
		}
	})

	t.Run("ServeFile_NonExistentBasePath", func(t *testing.T) {
		content, contentType, err := uiService.ServeFile("nonexistent", "index.html")
		if err == nil {
			t.Error("Expected error for non-existent base path")
		}
		if content != nil {
			t.Error("Expected nil content for non-existent base path")
		}
		if contentType != "" {
			t.Error("Expected empty content type for non-existent base path")
		}
	})
}
