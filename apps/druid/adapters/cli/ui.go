package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
)

var uiServeRoot string
var uiServeListen string
var uiPublishRoot string
var uiPublishSource string
var uiPublishBucket string
var uiPublishRegion string
var uiPublishEndpoint string
var uiPublishKeyPrefix string

var UICommand = &cobra.Command{
	Use:    "ui",
	Short:  "Serve and publish Druid UI packages",
	Hidden: true,
}

var UIServeCommand = &cobra.Command{
	Use:   "serve",
	Short: "Serve published Druid UI packages",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := filepath.Abs(uiServeRoot)
		if err != nil {
			return err
		}
		fileServer := http.FileServer(http.Dir(root))
		return http.ListenAndServe(uiServeListen, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			fileServer.ServeHTTP(w, r)
		}))
	},
}

var UIPublishS3Command = &cobra.Command{
	Use:   "publish-s3",
	Short: "Upload a built Druid UI WASM package to S3",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		hash, err := publishUIPackageToS3(cmd.Context())
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, hash)
		return nil
	},
}

func init() {
	RootCmd.AddCommand(UICommand)
	UICommand.AddCommand(UIServeCommand, UIPublishS3Command)
	UIServeCommand.Flags().StringVar(&uiServeRoot, "root", "/packages", "Directory containing published packages")
	UIServeCommand.Flags().StringVar(&uiServeListen, "listen", ":8085", "Static package server listen address")
	UIPublishS3Command.Flags().StringVar(&uiPublishRoot, "root", "/scroll", "Mounted scroll root")
	UIPublishS3Command.Flags().StringVar(&uiPublishSource, "source", "", "Scroll-root-relative .wasm package path")
	UIPublishS3Command.Flags().StringVar(&uiPublishBucket, "bucket", "", "S3 bucket")
	UIPublishS3Command.Flags().StringVar(&uiPublishRegion, "region", "", "S3 region")
	UIPublishS3Command.Flags().StringVar(&uiPublishEndpoint, "endpoint", "", "Optional S3-compatible endpoint")
	UIPublishS3Command.Flags().StringVar(&uiPublishKeyPrefix, "key-prefix", "", "S3 key prefix")
	UIPublishS3Command.MarkFlagRequired("source")
	UIPublishS3Command.MarkFlagRequired("bucket")
	UIPublishS3Command.MarkFlagRequired("region")
	UIPublishS3Command.MarkFlagRequired("key-prefix")
}

func publishUIPackageToS3(ctx context.Context) (string, error) {
	source, err := cleanUIPackageSource(uiPublishSource)
	if err != nil {
		return "", err
	}
	filePath := filepath.Join(uiPublishRoot, filepath.FromSlash(source))
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("UI package not found at %s; run the build command first", source)
		}
		return "", err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(uiPublishRegion))
	if err != nil {
		return "", err
	}
	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		if uiPublishEndpoint != "" {
			options.BaseEndpoint = aws.String(uiPublishEndpoint)
			options.UsePathStyle = true
		}
	})
	key := path.Join(strings.Trim(uiPublishKeyPrefix, "/"), hash, "app.wasm")
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(uiPublishBucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(data),
		ContentType:  aws.String("application/wasm"),
		CacheControl: aws.String("public, max-age=31536000, immutable"),
	})
	if err != nil {
		return "", err
	}
	return hash, nil
}

func cleanUIPackageSource(source string) (string, error) {
	if filepath.IsAbs(source) {
		return "", fmt.Errorf("ui package source must be relative to the scroll root")
	}
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(source, "/")))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("ui package source must stay inside the scroll root")
	}
	if filepath.Ext(cleaned) != ".wasm" {
		return "", fmt.Errorf("ui package source must point to a .wasm file")
	}
	return cleaned, nil
}
