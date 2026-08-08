package docker

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
	return uipackage.PresignPut(ctx, action.SHA256, b.uiPackageS3Config(action))
}

func (b *Backend) CompleteUIPackageUpload(ctx context.Context, action ports.RuntimeUIPackageUploadAction) (ports.RuntimeUIPackageResult, error) {
	if err := b.config.ValidateForUIPublishing(); err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	config := b.uiPackageS3Config(action)
	if err := uipackage.Verify(ctx, action.SHA256, action.Size, config); err != nil {
		return ports.RuntimeUIPackageResult{}, fmt.Errorf("verify uploaded UI package: %w", err)
	}
	return ports.RuntimeUIPackageResult{
		URL: strings.TrimRight(b.config.UIS3PublicBaseURL, "/") + "/" + path.Join(config.KeyPrefix, action.SHA256, "app.wasm"), SHA256: action.SHA256,
	}, nil
}

func (b *Backend) uiPackageS3Config(action ports.RuntimeUIPackageUploadAction) uipackage.S3Config {
	return uipackage.S3Config{
		Bucket: b.config.UIS3Bucket, Region: b.config.UIS3Region, Endpoint: b.config.UIS3Endpoint,
		KeyPrefix: strings.Trim(strings.Trim(b.config.UIS3Prefix, "/")+"/docker/"+action.RuntimeID+"/"+string(action.Scope), "/"),
		AccessKey: b.config.UIS3AccessKey, SecretKey: b.config.UIS3SecretKey, SessionToken: b.config.UIS3SessionToken,
	}
}
