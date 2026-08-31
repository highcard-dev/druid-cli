package docker

import (
	"context"
	"path"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/uipackage"
)

func (b *Backend) CreateUIPackageUpload(ctx context.Context, action ports.RuntimeUIPackageUploadAction) (ports.RuntimeUIPackageUpload, error) {
	if err := b.config.ValidateForUIPublishing(); err != nil {
		return ports.RuntimeUIPackageUpload{}, err
	}
	config := b.uiPackageS3Config(action)
	uploadURL, err := uipackage.PresignPut(ctx, uiPackageObjectName(action), config)
	if err != nil {
		return ports.RuntimeUIPackageUpload{}, err
	}
	return ports.RuntimeUIPackageUpload{UploadURL: uploadURL, URL: b.uiPackagePublicURL(config, action)}, nil
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
		KeyPrefix: strings.Trim(strings.Trim(b.config.UIS3Prefix, "/")+"/docker/"+action.RuntimeID+"/"+string(action.Scope), "/"),
		AccessKey: b.config.UIS3AccessKey, SecretKey: b.config.UIS3SecretKey, SessionToken: b.config.UIS3SessionToken,
	}
}
