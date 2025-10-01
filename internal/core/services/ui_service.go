package services

import (
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

	fileList := []string{}
	err := filepath.Walk(uiDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {

			if relPath, err := filepath.Rel(uiDir, path); err == nil {
				fileList = append(fileList, relPath)
			} else {
				// Fallback to original path if relative path calculation fails
				fileList = append(fileList, path)
			}
		}
		return nil
	})
	return fileList, err
}

// Ensure UiService implements UiServiceInterface at compile time
var _ ports.UiServiceInterface = (*UiService)(nil)
