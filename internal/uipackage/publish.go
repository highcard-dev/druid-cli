package uipackage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
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

func objectURL(config S3Config, objectName string) (*url.URL, error) {
	key := path.Join(strings.Trim(config.KeyPrefix, "/"), objectName)
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://" + config.Bucket + ".s3." + config.Region + ".amazonaws.com"
	} else {
		key = path.Join(config.Bucket, key)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid UI package S3 endpoint")
	}
	parsed.Path = path.Join(parsed.Path, key)
	return parsed, nil
}

// PresignPut issues a short-lived URL for a one-shot runtime command.
// S3 requires the uploader to provide a SHA-256 checksum and verifies it
// against the uploaded bytes before accepting the object.
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
	cfg, err := loadConfig(ctx, config)
	if err != nil {
		return "", err
	}
	uploadURL, err := objectURL(config, objectName)
	if err != nil {
		return "", err
	}
	query := uploadURL.Query()
	query.Set("X-Amz-Expires", "300")
	uploadURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL.String(), nil)
	if err != nil {
		return "", err
	}
	checksum := base64.StdEncoding.EncodeToString(sha256sum)
	request.Header.Set("Content-Type", "application/wasm")
	request.Header.Set("Cache-Control", "public, max-age=31536000, immutable")
	request.Header.Set("Content-MD5", action.ContentMD5)
	request.Header.Set("x-amz-sdk-checksum-algorithm", "SHA256")
	request.Header.Set("x-amz-checksum-sha256", checksum)
	credentials, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return "", err
	}
	presigned, signedHeaders, err := v4.NewSigner().PresignHTTP(ctx, credentials, request, "UNSIGNED-PAYLOAD", "s3", config.Region, time.Now(), func(options *v4.SignerOptions) {
		options.DisableHeaderHoisting = true
	})
	if err != nil {
		return "", err
	}
	if signedHeaders.Get("Content-Md5") != action.ContentMD5 || signedHeaders.Get("X-Amz-Checksum-Sha256") != checksum || signedHeaders.Get("X-Amz-Sdk-Checksum-Algorithm") != "SHA256" {
		return "", fmt.Errorf("UI package presigned request did not bind required integrity headers")
	}
	return presigned, nil
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
