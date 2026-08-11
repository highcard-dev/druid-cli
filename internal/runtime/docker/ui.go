package docker

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/uipackage"
)

func (b *Backend) PrepareUIPackageUpload(ctx context.Context, action ports.RuntimeUIPackageUploadAction) (ports.RuntimeUIPackageUploadCapability, error) {
	if err := b.config.ValidateForUIPublishing(); err != nil {
		return ports.RuntimeUIPackageUploadCapability{}, err
	}
	config := b.uiPackageS3Config(action)
	uploadURL, err := uipackage.PresignPut(ctx, uiPackageObjectName(action), action, config)
	if err != nil {
		return ports.RuntimeUIPackageUploadCapability{}, err
	}
	verifyURL, err := uipackage.EndpointObjectURL(config, uiPackageObjectName(action))
	if err != nil {
		return ports.RuntimeUIPackageUploadCapability{}, err
	}
	if verifyURL == "" {
		verifyURL = b.uiPackagePublicURL(config, action)
	}
	return ports.RuntimeUIPackageUploadCapability{UploadURL: uploadURL, VerifyURL: verifyURL}, nil
}

func (b *Backend) CompleteUIPackageUpload(ctx context.Context, action ports.RuntimeUIPackageUploadAction) (ports.RuntimeUIPackageResult, error) {
	if err := b.config.ValidateForUIPublishing(); err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	config := b.uiPackageS3Config(action)
	objectName := uiPackageObjectName(action)
	sha256, err := uipackage.Inspect(ctx, objectName, action, config)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, fmt.Errorf("verify uploaded UI package: %w", err)
	}
	return ports.RuntimeUIPackageResult{
		URL: b.uiPackagePublicURL(config, action), SHA256: sha256,
	}, nil
}

func (b *Backend) uiPackagePublicURL(config uipackage.S3Config, action ports.RuntimeUIPackageUploadAction) string {
	return strings.TrimRight(b.config.UIS3PublicBaseURL, "/") + "/" + path.Join(config.KeyPrefix, uiPackageObjectName(action))
}

func uiPackageObjectName(action ports.RuntimeUIPackageUploadAction) string {
	return path.Join(action.RequestID, "app.wasm")
}

func (b *Backend) uiPackageS3Config(action ports.RuntimeUIPackageUploadAction) uipackage.S3Config {
	return uipackage.S3Config{
		Bucket: b.config.UIS3Bucket, Region: b.config.UIS3Region, Endpoint: b.config.UIS3Endpoint,
		VerifyEndpoint: b.config.UIS3DaemonEndpoint,
		KeyPrefix:      strings.Trim(strings.Trim(b.config.UIS3Prefix, "/")+"/docker/"+action.RuntimeID+"/"+string(action.Scope), "/"),
		AccessKey:      b.config.UIS3AccessKey, SecretKey: b.config.UIS3SecretKey, SessionToken: b.config.UIS3SessionToken,
	}
}
