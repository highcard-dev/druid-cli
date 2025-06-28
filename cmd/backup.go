package cmd

import (
	"fmt"
	"os"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/core/services"
	snapshotServices "github.com/highcard-dev/daemon/internal/core/services/snapshot"

	"github.com/spf13/cobra"
)

var compressionLevel int

var s3AccessKey string
var s3SecretKey string
var s3Bucket string
var s3Endpoint string

var BackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backup the current scroll",
	Long:  "Backup the current scroll, by specifying the s3 bucket to backup to or a pre-signed url",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		destination := args[0]

		scrollService, err := services.NewScrollService(cwd)
		if scrollService == nil {
			return fmt.Errorf("failed to load scroll - %w", err)
		}

		snapshotService := snapshotServices.NewSnapshotService()

		var s3Destination ports.S3Destination

		if s3AccessKey != "" && s3SecretKey != "" && s3Bucket != "" {
			s3Destination = ports.S3Destination{
				AccessKey: s3AccessKey,
				SecretKey: s3SecretKey,
				Bucket:    s3Bucket,
				Endpoint:  s3Endpoint,
			}
		}

		options := ports.SnapshotOptions{
			CompressionLevel: compressionLevel,
			S3Destination:    &s3Destination,
		}

		return snapshotService.Snapshot(scrollService.GetCwd(), destination, options)
	},
}

func init() {

	accessKeyEnv := os.Getenv("DRUID_S3_ACCESS_KEY")
	secretKeyEnv := os.Getenv("DRUID_S3_SECRET_KEY")
	bucketEnv := os.Getenv("DRUID_S3_BUCKET")
	endpointEnv := os.Getenv("DRUID_S3_ENDPOINT")

	BackupCmd.Flags().IntVarP(&compressionLevel, "compression-level", "", 1, "Compression level for the backup")
	BackupCmd.Flags().StringVarP(&s3AccessKey, "s3-access-key", "", accessKeyEnv, "S3 access key. You can also set the DRUID_S3_ACCESS_KEY environment variable")
	BackupCmd.Flags().StringVarP(&s3SecretKey, "s3-secret-key", "", secretKeyEnv, "S3 secret key. You can also set the DRUID_S3_SECRET_KEY environment variable")
	BackupCmd.Flags().StringVarP(&s3Bucket, "s3-bucket", "", bucketEnv, "S3 bucket. You can also set the DRUID_S3_BUCKET environment variable")
	BackupCmd.Flags().StringVarP(&s3Endpoint, "s3-endpoint", "", endpointEnv, "S3 endpoint. You can also set the DRUID_S3_ENDPOINT environment variable")
}
