package kubernetes

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/uipackage"
)

func (b *Backend) PrepareUIPackageUpload(ctx context.Context, action ports.RuntimeUIPackageUploadAction) (string, error) {
	if err := b.config.ValidateForUIPublishing(); err != nil {
		return "", err
	}
	namespace, _, err := parseRef(action.RootRef)
	if err != nil {
		return "", err
	}
	return uipackage.PresignPut(ctx, uiPackageObjectName(action), b.uiPackageS3Config(namespace, action))
}

func (b *Backend) CompleteUIPackageUpload(ctx context.Context, action ports.RuntimeUIPackageUploadAction) (ports.RuntimeUIPackageResult, error) {
	if err := b.config.ValidateForUIPublishing(); err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	namespace, _, err := parseRef(action.RootRef)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	config := b.uiPackageS3Config(namespace, action)
	objectName := uiPackageObjectName(action)
	sha256, err := uipackage.Inspect(ctx, objectName, config)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, fmt.Errorf("verify uploaded UI package: %w", err)
	}
	return ports.RuntimeUIPackageResult{
		URL:    strings.TrimRight(b.config.UIS3PublicBaseURL, "/") + "/" + path.Join(config.KeyPrefix, objectName),
		SHA256: sha256,
	}, nil
}

func uiPackageObjectName(action ports.RuntimeUIPackageUploadAction) string {
	return path.Join(action.RequestID, "app.wasm")
}

func (b *Backend) uiPackageS3Config(namespace string, action ports.RuntimeUIPackageUploadAction) uipackage.S3Config {
	return uipackage.S3Config{
		Bucket: b.config.UIS3Bucket, Region: b.config.UIS3Region, Endpoint: b.config.UIS3Endpoint,
		VerifyEndpoint: b.config.UIS3DaemonEndpoint,
		KeyPrefix:      strings.Trim(strings.Trim(b.config.UIS3Prefix, "/")+"/"+namespace+"/"+action.RuntimeID+"/"+string(action.Scope), "/"),
		AccessKey:      b.config.UIS3AccessKey, SecretKey: b.config.UIS3SecretKey, SessionToken: b.config.UIS3SessionToken,
	}
}
