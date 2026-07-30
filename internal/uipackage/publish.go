package uipackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Config struct {
	Bucket       string
	Region       string
	Endpoint     string
	KeyPrefix    string
	AccessKey    string
	SecretKey    string
	SessionToken string
}

// Upload stores content at a content-addressed key and returns its SHA-256.
func Upload(ctx context.Context, content []byte, config S3Config) (string, error) {
	if config.Bucket == "" || config.Region == "" {
		return "", fmt.Errorf("ui package S3 bucket and region are required")
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(config.Region))
	if err != nil {
		return "", err
	}
	if config.AccessKey != "" || config.SecretKey != "" {
		cfg.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.AccessKey, config.SecretKey, config.SessionToken))
	}
	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		if config.Endpoint != "" {
			options.BaseEndpoint = aws.String(config.Endpoint)
			options.UsePathStyle = true
		}
	})
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
