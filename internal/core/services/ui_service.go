package services

import (
	"io"
	"net/http"
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
			fileList = append(fileList, path)
		}
		return nil
	})
	return fileList, err
}

// ServeFile serves a file from the specified base path and file name
// Returns file content, content type, and any error
func (us *UiService) ServeFile(basePath string, fileName string) ([]byte, string, error) {
	scrollDir := us.scrollService.GetDir()
	fullPath := filepath.Join(scrollDir, basePath, fileName)

	// Read the file
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, "", err
	}

	// Determine content type based on file extension and content
	contentType := http.DetectContentType(content)

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return content, contentType, nil
}

// Ensure UiService implements UiServiceInterface at compile time
var _ ports.UiServiceInterface = (*UiService)(nil)
