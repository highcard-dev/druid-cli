package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/uipackage"
)

func (b *Backend) PublishUIPackage(ctx context.Context, action ports.RuntimeUIPackageAction) (ports.RuntimeUIPackageResult, error) {
	if err := b.config.ValidateForUIPublishing(); err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	namespace, _, err := parseRef(action.RootRef)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	status, err := b.DevStatus(ctx, action.RootRef)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	if status != ports.RuntimeDevStatusReady {
		return ports.RuntimeUIPackageResult{}, fmt.Errorf("Druid Developer Mode must be ready before publishing a UI package (current status: %s)", status)
	}
	info, err := b.uiPackageInfo(ctx, namespace, action.RootRef, action.SourcePath)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	keyPrefix := strings.Trim(strings.Trim(b.config.UIS3Prefix, "/")+"/"+namespace+"/"+action.RuntimeID+"/"+string(action.Scope), "/")
	config := uipackage.S3Config{
		Bucket:         b.config.UIS3Bucket,
		Region:         b.config.UIS3Region,
		Endpoint:       b.config.UIS3Endpoint,
		VerifyEndpoint: b.config.UIS3DaemonEndpoint,
		KeyPrefix:      keyPrefix,
		AccessKey:      b.config.UIS3AccessKey,
		SecretKey:      b.config.UIS3SecretKey,
		SessionToken:   b.config.UIS3SessionToken,
	}
	uploadURL, err := uipackage.PresignPut(ctx, info.SHA256, config)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	if err := b.publishUIPackage(ctx, namespace, action.RootRef, info, uploadURL); err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	if err := uipackage.Verify(ctx, info.SHA256, info.Size, config); err != nil {
		return ports.RuntimeUIPackageResult{}, fmt.Errorf("verify uploaded UI package: %w", err)
	}
	key := path.Join(keyPrefix, info.SHA256, "app.wasm")
	return ports.RuntimeUIPackageResult{
		URL:    strings.TrimRight(b.config.UIS3PublicBaseURL, "/") + "/" + key,
		Path:   action.SourcePath,
		SHA256: info.SHA256,
	}, nil
}

type uiPackageInfo struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (b *Backend) uiPackageInfo(ctx context.Context, namespace string, root string, sourcePath string) (uiPackageInfo, error) {
	endpoint := "/internal/v1/ui/info?path=" + url.QueryEscape(sourcePath)
	response, err := b.devRequest(ctx, namespace, root, http.MethodGet, endpoint, nil)
	if err != nil {
		return uiPackageInfo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return uiPackageInfo{}, fmt.Errorf("dev service returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var info uiPackageInfo
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		return uiPackageInfo{}, err
	}
	if info.Path != sourcePath || info.SHA256 == "" || info.Size < 0 {
		return uiPackageInfo{}, fmt.Errorf("dev service returned invalid UI package metadata")
	}
	return info, nil
}

func (b *Backend) publishUIPackage(ctx context.Context, namespace string, root string, info uiPackageInfo, uploadURL string) error {
	body, err := json.Marshal(struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
		URL    string `json:"url"`
	}{Path: info.Path, SHA256: info.SHA256, Size: info.Size, URL: uploadURL})
	if err != nil {
		return err
	}
	response, err := b.devRequest(ctx, namespace, root, http.MethodPost, "/internal/v1/ui/publish", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("dev service upload failed: %s", strings.TrimSpace(string(message)))
	}
	return nil
}
