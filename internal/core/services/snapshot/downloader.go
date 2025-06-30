package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// FileInfo contains information about a remote file
type FileInfo struct {
	Exists      bool
	Size        int64
	ContentType string
}

// CheckFileInfo checks if a file exists and returns its size and content type.
// Only works for HTTP/HTTPS URLs. Returns error for local files or unsupported schemes.
func CheckFileInfo(ctx context.Context, source string) (*FileInfo, error) {
	// Only support HTTP/HTTPS URLs
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		return nil, fmt.Errorf("file info check only supported for HTTP/HTTPS URLs")
	}

	// Create HTTP HEAD request with context
	req, err := http.NewRequestWithContext(ctx, "HEAD", source, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HEAD request: %w", err)
	}

	// Make the HTTP HEAD request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to check file info for %s: %w", source, err)
	}
	defer resp.Body.Close()

	info := &FileInfo{
		Exists:      resp.StatusCode == http.StatusOK,
		ContentType: resp.Header.Get("Content-Type"),
	}

	// Parse content length if available
	if contentLength := resp.Header.Get("Content-Length"); contentLength != "" {
		if size, err := strconv.ParseInt(contentLength, 10, 64); err == nil {
			info.Size = size
		}
	}

	// If file doesn't exist, return the info but no error
	if !info.Exists {
		return info, nil
	}

	return info, nil
}

// DownloadToDirectory downloads and unpacks a snapshot to the specified directory.
func DownloadToDirectory(ctx context.Context, dir string, source string, basicTracker *BasicTracker) error {
	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Check if source is a URL or local file
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {

		return downloadAndExtractFromURL(ctx, dir, source, basicTracker)
	}

	return fmt.Errorf("scheme is not supported: %s", source)
}

// downloadAndExtractFromURL downloads a file from URL and extracts it directly
func downloadAndExtractFromURL(ctx context.Context, dir, url string, basicTracker *BasicTracker) error {

	info, err := CheckFileInfo(ctx, url)
	if err != nil {
		return fmt.Errorf("failed to check file info for %s: %w", url, err)
	}
	if !info.Exists {
		return fmt.Errorf("file does not exist at %s", url)
	}

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Make the HTTP request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download: HTTP %d", resp.StatusCode)
	}

	tracker := NewProgressReader(info.Size, resp.Body, basicTracker)

	// Extract directly from the response body stream
	return extractFromReader(dir, tracker, url)
}

// extractFromReader determines the file type and extracts from a reader
func extractFromReader(dir string, reader io.Reader, filename string) error {
	if strings.HasSuffix(strings.ToLower(filename), ".tar.gz") || strings.HasSuffix(strings.ToLower(filename), ".tgz") {
		return extractTarGzFromReader(dir, reader)
	} else if strings.HasSuffix(strings.ToLower(filename), ".tar") {
		return extractTarFromReader(dir, reader)
	}

	return fmt.Errorf("unsupported file format: %s", filename)
}
