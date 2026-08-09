package uipackage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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

func newClient(ctx context.Context, config S3Config) (*s3.Client, error) {
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(config.Region))
	if err != nil {
		return nil, err
	}
	if config.AccessKey != "" || config.SecretKey != "" {
		cfg.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.AccessKey, config.SecretKey, config.SessionToken))
	}
	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		if config.Endpoint != "" {
			options.BaseEndpoint = aws.String(config.Endpoint)
			options.UsePathStyle = true
		}
	}), nil
}

// PresignPut issues a short-lived URL for a one-shot runtime command.
// S3 requires the uploader to provide a SHA-256 checksum and verifies it
// against the uploaded bytes before accepting the object.
func PresignPut(ctx context.Context, objectName string, config S3Config) (string, error) {
	if config.Bucket == "" || config.Region == "" || objectName == "" {
		return "", fmt.Errorf("ui package S3 bucket, region, and object name are required")
	}
	client, err := newClient(ctx, config)
	if err != nil {
		return "", err
	}
	request, err := s3.NewPresignClient(client).PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(config.Bucket),
		Key:               aws.String(path.Join(strings.Trim(config.KeyPrefix, "/"), objectName)),
		ContentType:       aws.String("application/wasm"),
		CacheControl:      aws.String("public, max-age=31536000, immutable"),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	}, s3.WithPresignExpires(5*time.Minute))
	if err != nil {
		return "", err
	}
	return request.URL, nil
}

// Inspect returns the checksum verified by the object store. A
// package without a server-returned SHA-256 checksum is never published.
func Inspect(ctx context.Context, objectName string, config S3Config) (string, error) {
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
	if object.ChecksumSHA256 == nil || *object.ChecksumSHA256 == "" {
		return "", fmt.Errorf("uploaded UI package is missing a verified SHA-256 checksum")
	}
	sum, err := base64.StdEncoding.DecodeString(*object.ChecksumSHA256)
	if err != nil || len(sum) != sha256.Size {
		return "", fmt.Errorf("uploaded UI package has an invalid SHA-256 checksum")
	}
	return hex.EncodeToString(sum), nil
}
