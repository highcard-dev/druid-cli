package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/uipackage"
)

type uiPackageInfo struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (b *Backend) PublishUIPackage(ctx context.Context, action ports.RuntimeUIPackageAction) (ports.RuntimeUIPackageResult, error) {
	if err := b.config.ValidateForUIPublishing(); err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	if action.RuntimeID == "" || action.RootRef == "" || action.SourcePath == "" {
		return ports.RuntimeUIPackageResult{}, fmt.Errorf("ui package publish requires runtime id, root, and source path")
	}
	status, err := b.DevStatus(ctx, action.RootRef)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	if status != ports.RuntimeDevStatusReady {
		return ports.RuntimeUIPackageResult{}, fmt.Errorf("Druid Developer Mode must be ready before publishing a UI package (current status: %s)", status)
	}
	info, err := b.uiPackageInfo(ctx, action.RootRef, action.SourcePath)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	keyPrefix := strings.Trim(strings.Trim(b.config.UIS3Prefix, "/")+"/docker/"+action.RuntimeID+"/"+string(action.Scope), "/")
	config := uipackage.S3Config{
		Bucket: b.config.UIS3Bucket, Region: b.config.UIS3Region, Endpoint: b.config.UIS3Endpoint,
		KeyPrefix: keyPrefix, AccessKey: b.config.UIS3AccessKey, SecretKey: b.config.UIS3SecretKey, SessionToken: b.config.UIS3SessionToken,
	}
	uploadURL, err := uipackage.PresignPut(ctx, info.SHA256, config)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	if err := b.publishUIPackage(ctx, action.RootRef, info, uploadURL); err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	if err := uipackage.Verify(ctx, info.SHA256, info.Size, config); err != nil {
		return ports.RuntimeUIPackageResult{}, fmt.Errorf("verify uploaded UI package: %w", err)
	}
	return ports.RuntimeUIPackageResult{
		URL:  strings.TrimRight(b.config.UIS3PublicBaseURL, "/") + "/" + path.Join(keyPrefix, info.SHA256, "app.wasm"),
		Path: action.SourcePath, SHA256: info.SHA256,
	}, nil
}

func (b *Backend) uiPackageInfo(ctx context.Context, root string, sourcePath string) (uiPackageInfo, error) {
	response, err := b.devRequest(ctx, root, http.MethodGet, "/internal/v1/ui/info?path="+url.QueryEscape(sourcePath), nil)
	if err != nil {
		return uiPackageInfo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return uiPackageInfo{}, fmt.Errorf("dev container returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var info uiPackageInfo
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		return uiPackageInfo{}, err
	}
	if info.Path != sourcePath || info.SHA256 == "" || info.Size < 0 {
		return uiPackageInfo{}, fmt.Errorf("dev container returned invalid UI package metadata")
	}
	return info, nil
}

func (b *Backend) publishUIPackage(ctx context.Context, root string, info uiPackageInfo, uploadURL string) error {
	body, err := json.Marshal(struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
		URL    string `json:"url"`
	}{info.Path, info.SHA256, info.Size, uploadURL})
	if err != nil {
		return err
	}
	response, err := b.devRequest(ctx, root, http.MethodPost, "/internal/v1/ui/publish", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("dev container upload failed: %s", strings.TrimSpace(string(message)))
	}
	return nil
}

func (b *Backend) devURL(ctx context.Context, root string) (string, error) {
	inspect, err := b.client.ContainerInspect(ctx, ContainerName(root, "dev"))
	if err != nil {
		return "", err
	}
	bindings := inspect.NetworkSettings.Ports["8084/tcp"]
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return "", fmt.Errorf("dev container has no WebDAV port binding")
	}
	host := bindings[0].HostIP
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, bindings[0].HostPort), nil
}

func (b *Backend) devRequest(ctx context.Context, root string, method string, endpoint string, body io.Reader) (*http.Response, error) {
	baseURL, err := b.devURL(ctx, root)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, baseURL+endpoint, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return http.DefaultClient.Do(request)
}

func (b *Backend) devHealth(ctx context.Context, root string) error {
	response, err := b.devRequest(ctx, root, http.MethodGet, "/health", nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("dev container health returned %s", response.Status)
	}
	return nil
}

func (b *Backend) devContainer(ctx context.Context, root string) (container.InspectResponse, error) {
	return b.client.ContainerInspect(ctx, ContainerName(root, "dev"))
}
