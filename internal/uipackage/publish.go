package uipackage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type S3Config struct {
	Bucket         string
	Region         string
	Endpoint       string
	KeyPrefix      string
	AccessKey      string
	SecretKey      string
	SessionToken   string
	VerifyEndpoint string
}

func loadConfig(ctx context.Context, config S3Config) (aws.Config, error) {
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(config.Region))
	if err != nil {
		return aws.Config{}, err
	}
	if config.AccessKey != "" || config.SecretKey != "" {
		cfg.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.AccessKey, config.SecretKey, config.SessionToken))
	}
	return cfg, nil
}

func newClient(ctx context.Context, config S3Config) (*s3.Client, error) {
	cfg, err := loadConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		if config.Endpoint != "" {
			options.BaseEndpoint = aws.String(config.Endpoint)
			options.UsePathStyle = true
		}
	}), nil
}

// EndpointObjectURL returns the path-style URL reachable by an uploader that
// uses a configured S3-compatible endpoint.
func EndpointObjectURL(config S3Config, objectName string) (string, error) {
	if config.Endpoint == "" {
		return "", nil
	}
	return url.JoinPath(config.Endpoint, config.Bucket, path.Join(strings.Trim(config.KeyPrefix, "/"), objectName))
}

// PresignPut issues a short-lived URL for a one-shot runtime command.
// Content-MD5 binds the exact bytes while remaining compatible with S3 stores
// that do not implement the newer x-amz-checksum-* request contract.
func PresignPut(ctx context.Context, objectName string, action ports.RuntimeUIPackageUploadAction, config S3Config) (string, error) {
	if config.Bucket == "" || config.Region == "" || objectName == "" || action.SHA256 == "" || action.ContentMD5 == "" {
		return "", fmt.Errorf("ui package S3 bucket, region, object name, and digests are required")
	}
	sha256sum, err := hex.DecodeString(action.SHA256)
	if err != nil || len(sha256sum) != sha256.Size {
		return "", fmt.Errorf("invalid UI package SHA-256")
	}
	md5, err := base64.StdEncoding.DecodeString(action.ContentMD5)
	if err != nil || len(md5) != 16 {
		return "", fmt.Errorf("invalid UI package Content-MD5")
	}
	client, err := newClient(ctx, config)
	if err != nil {
		return "", err
	}
	presigned, err := s3.NewPresignClient(client).PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(config.Bucket),
		Key:          aws.String(path.Join(strings.Trim(config.KeyPrefix, "/"), objectName)),
		ContentType:  aws.String("application/wasm"),
		CacheControl: aws.String("public, max-age=31536000, immutable"),
		ContentMD5:   aws.String(action.ContentMD5),
	}, func(options *s3.PresignOptions) {
		options.Expires = 5 * time.Minute
	})
	if err != nil {
		return "", err
	}
	if presigned.SignedHeader.Get("Content-Md5") != action.ContentMD5 {
		return "", fmt.Errorf("UI package presigned request did not bind required integrity headers")
	}
	return presigned.URL, nil
}

// Inspect verifies the server's stored bytes using its ETag. The publish Job
// separately downloads the immutable object and compares SHA-256 before it
// exits, which covers S3-compatible stores that do not return checksum fields.
func Inspect(ctx context.Context, objectName string, action ports.RuntimeUIPackageUploadAction, config S3Config) (string, error) {
	if config.VerifyEndpoint != "" {
		config.Endpoint = config.VerifyEndpoint
	}
	client, err := newClient(ctx, config)
	if err != nil {
		return "", err
	}
	object, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket:       aws.String(config.Bucket),
		Key:          aws.String(path.Join(strings.Trim(config.KeyPrefix, "/"), objectName)),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return "", err
	}
	if object.ContentLength == nil || *object.ContentLength <= 0 {
		return "", fmt.Errorf("uploaded UI package is empty")
	}
	md5, err := base64.StdEncoding.DecodeString(action.ContentMD5)
	if err != nil || len(md5) != 16 {
		return "", fmt.Errorf("invalid expected UI package Content-MD5")
	}
	if object.ETag == nil || strings.Trim(*object.ETag, "\"") != hex.EncodeToString(md5) {
		return "", fmt.Errorf("uploaded UI package ETag does not match the signed Content-MD5")
	}
	if object.ChecksumSHA256 == nil || *object.ChecksumSHA256 == "" {
		return action.SHA256, nil
	}
	sum, err := base64.StdEncoding.DecodeString(*object.ChecksumSHA256)
	if err != nil || len(sum) != sha256.Size {
		return "", fmt.Errorf("uploaded UI package has an invalid SHA-256 checksum")
	}
	if got := hex.EncodeToString(sum); got != action.SHA256 {
		return "", fmt.Errorf("uploaded UI package SHA-256 does not match the signed checksum")
	}
	return action.SHA256, nil
}
