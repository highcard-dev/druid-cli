package uipackage

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// The one-shot job needs time to be scheduled and pull its image before it can
// upload, while still keeping the bearer URL short lived.
const uploadURLExpiry = 20 * time.Minute

type S3Config struct {
	Bucket       string
	Region       string
	Endpoint     string
	KeyPrefix    string
	AccessKey    string
	SecretKey    string
	SessionToken string
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

// PresignPut issues a short-lived URL for uploading one UI package object.
func PresignPut(ctx context.Context, objectName string, config S3Config) (string, error) {
	if config.Bucket == "" || config.Region == "" || objectName == "" {
		return "", fmt.Errorf("ui package S3 bucket, region, and object name are required")
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
	}, func(options *s3.PresignOptions) {
		options.Expires = uploadURLExpiry
	})
	if err != nil {
		return "", err
	}
	return presigned.URL, nil
}
