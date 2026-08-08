package uipackage

import (
	"bytes"
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

func checksumFromHash(hash string) (string, error) {
	sum, err := hex.DecodeString(hash)
	if err != nil || len(sum) != sha256.Size {
		return "", fmt.Errorf("invalid UI package SHA-256")
	}
	return base64.StdEncoding.EncodeToString(sum), nil
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

// PresignPut returns a short-lived URL for exactly one content-addressed UI package.
func PresignPut(ctx context.Context, hash string, config S3Config) (string, error) {
	if config.Bucket == "" || config.Region == "" || hash == "" {
		return "", fmt.Errorf("ui package S3 bucket, region, and hash are required")
	}
	checksum, err := checksumFromHash(hash)
	if err != nil {
		return "", err
	}
	client, err := newClient(ctx, config)
	if err != nil {
		return "", err
	}
	request, err := s3.NewPresignClient(client).PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:         aws.String(config.Bucket),
		Key:            aws.String(path.Join(strings.Trim(config.KeyPrefix, "/"), hash, "app.wasm")),
		ContentType:    aws.String("application/wasm"),
		CacheControl:   aws.String("public, max-age=31536000, immutable"),
		ChecksumSHA256: aws.String(checksum),
	}, s3.WithPresignExpires(5*time.Minute))
	if err != nil {
		return "", err
	}
	return request.URL, nil
}

// Verify confirms the dev agent stored the exact checksum and expected size.
func Verify(ctx context.Context, hash string, size int64, config S3Config) error {
	checksum, err := checksumFromHash(hash)
	if err != nil {
		return err
	}
	if config.VerifyEndpoint != "" {
		config.Endpoint = config.VerifyEndpoint
	}
	client, err := newClient(ctx, config)
	if err != nil {
		return err
	}
	object, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket:       aws.String(config.Bucket),
		Key:          aws.String(path.Join(strings.Trim(config.KeyPrefix, "/"), hash, "app.wasm")),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return err
	}
	if object.ContentLength == nil || *object.ContentLength != size {
		return fmt.Errorf("uploaded UI package size does not match")
	}
	// S3-compatible stores such as MinIO may validate the signed checksum on
	// upload without returning it from HeadObject. A returned checksum must match.
	if object.ChecksumSHA256 != nil && *object.ChecksumSHA256 != checksum {
		return fmt.Errorf("uploaded UI package checksum does not match")
	}
	return nil
}

// Upload stores content at a content-addressed key and returns its SHA-256.
func Upload(ctx context.Context, content []byte, config S3Config) (string, error) {
	if config.Bucket == "" || config.Region == "" {
		return "", fmt.Errorf("ui package S3 bucket and region are required")
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	client, err := newClient(ctx, config)
	if err != nil {
		return "", err
	}
	key := path.Join(strings.Trim(config.KeyPrefix, "/"), hash, "app.wasm")
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(config.Bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(content),
		ContentType:  aws.String("application/wasm"),
		CacheControl: aws.String("public, max-age=31536000, immutable"),
	})
	if err != nil {
		return "", err
	}
	return hash, nil
}
