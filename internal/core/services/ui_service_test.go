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

		// Verify all expected files are present (now expecting relative paths)
		expectedFiles := []string{
			"public/index.html",
			"public/style.css",
			"public/script.js",
			"public/assets/image.png",
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
