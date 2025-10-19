package services

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/highcard-dev/daemon/internal/core/ports"
)

// UiService handles serving UI-related files and assets
type UiService struct {
	scrollService ports.ScrollServiceInterface
}

// NewUiService creates a new instance of UiService
func NewUiService(scrollService ports.ScrollServiceInterface) ports.UiServiceInterface {
	return &UiService{
		scrollService: scrollService,
	}
}

// GetIndex serves an index file from the specified file path
// This method validates the file exists and is accessible before serving
func (us *UiService) GetIndex(filePath string) ([]string, error) {
	scrollDir := us.scrollService.GetDir()
	uiDir := scrollDir + "/" + filePath

	// Check if directory exists
	info, err := os.Stat(uiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, err // This will be handled in the handler for a 500 error, but let's make it more informative
	}
	if !info.IsDir() {
		return nil, os.ErrInvalid
	}

	fileList := []string{}
	walkErr := filepath.Walk(uiDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if relPath, err := filepath.Rel(uiDir, path); err == nil {
				fileList = append(fileList, relPath)
			} else {
				fileList = append(fileList, path)
			}
		}
		return nil
	})
	if walkErr != nil {
		// Return a wrapped error for more informative 500 error
		return nil, fmt.Errorf("failed to walk directory '%s': %w", uiDir, walkErr)
	}
	return fileList, nil
}

// Ensure UiService implements UiServiceInterface at compile time
var _ ports.UiServiceInterface = (*UiService)(nil)
