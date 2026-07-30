package kubernetes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/uipackage"
)

func (b *Backend) PublishUIPackage(ctx context.Context, action ports.RuntimeUIPackageAction) (ports.RuntimeUIPackageResult, error) {
	if err := b.config.ValidateForUIPublishing(); err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	namespace, pvc, err := parseRef(action.RootRef)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	temporaryDevServer, err := b.ensureUIPackageDevServer(ctx, namespace, pvc, action)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	if temporaryDevServer {
		defer b.StopDev(context.Background(), action.RootRef)
	}
	content, err := b.fetchUIPackage(ctx, namespace, action.RootRef, action.SourcePath)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	keyPrefix := strings.Trim(strings.Trim(b.config.UIS3Prefix, "/")+"/"+namespace+"/"+action.RuntimeID+"/"+string(action.Scope), "/")
	hash, err := b.uploadUIPackage(ctx, content, uipackage.S3Config{
		Bucket:       b.config.UIS3Bucket,
		Region:       b.config.UIS3Region,
		Endpoint:     b.config.UIS3Endpoint,
		KeyPrefix:    keyPrefix,
		AccessKey:    b.config.UIS3AccessKey,
		SecretKey:    b.config.UIS3SecretKey,
		SessionToken: b.config.UIS3SessionToken,
	})
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	if hash == "" {
		return ports.RuntimeUIPackageResult{}, fmt.Errorf("ui package upload did not return a content hash")
	}
	key := path.Join(keyPrefix, hash, "app.wasm")
	return ports.RuntimeUIPackageResult{
		URL:    strings.TrimRight(b.config.UIS3PublicBaseURL, "/") + "/" + key,
		Path:   action.SourcePath,
		SHA256: hash,
	}, nil
}

func (b *Backend) ensureUIPackageDevServer(ctx context.Context, namespace string, pvc string, action ports.RuntimeUIPackageAction) (bool, error) {
	name := devStatefulSetName(action.RootRef)
	_, err := b.client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return false, nil
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	devAction := ports.RuntimeDevAction{
		RuntimeID:   action.RuntimeID,
		RootRef:     action.RootRef,
		MountPath:   "/scroll",
		Listen:      ":8084",
		DaemonToken: b.config.InternalToken,
	}
	server := devStatefulSetSpec(namespace, action.RootRef, pvc, b.config.PullImage, devAction, b.config.RegistrySecret)
	if _, err := b.client.AppsV1().StatefulSets(namespace).Create(ctx, server, metav1.CreateOptions{}); err != nil {
		return false, err
	}
	if err := b.reconcileService(ctx, devServiceSpec(namespace, action.RootRef, pvc)); err != nil {
		_ = b.StopDev(context.Background(), action.RootRef)
		return false, err
	}
	if err := b.waitForStatefulSet(ctx, namespace, name); err != nil {
		_ = b.StopDev(context.Background(), action.RootRef)
		return false, err
	}
	return true, nil
}

func (b *Backend) fetchUIPackage(ctx context.Context, namespace string, root string, sourcePath string) ([]byte, error) {
	service := serviceName(root, "dev", "webdav")
	if b.uiPackageFetcher != nil {
		return b.uiPackageFetcher(ctx, namespace, service, sourcePath, b.config.InternalToken)
	}
	if b.restConfig == nil || b.httpClient == nil {
		return nil, fmt.Errorf("kubernetes service proxy is unavailable")
	}
	segments := strings.Split(sourcePath, "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	proxyURL := strings.TrimRight(b.restConfig.Host, "/") + "/api/v1/namespaces/" + namespace + "/services/http:" + service + ":8084/proxy/internal/v1/ui/" + strings.Join(segments, "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+b.config.InternalToken)
	response, err := b.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("dev service returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(response.Body)
}

func (b *Backend) uploadUIPackage(ctx context.Context, content []byte, config uipackage.S3Config) (string, error) {
	if b.uiPackageUploader != nil {
		return b.uiPackageUploader(ctx, content, config)
	}
	return uipackage.Upload(ctx, content, config)
}
