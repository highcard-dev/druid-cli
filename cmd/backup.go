package cmd

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/core/services"
	"github.com/spf13/cobra"
)

var tgzTempDir string

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

		snapshotService := services.NewRestoreService()

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
			TempDir:       tempDir,
			S3Destination: &s3Destination,
		}

		return snapshotService.Snapshot(scrollService.GetCwd(), destination, options)
	},
}

func init() {
	BackupCmd.Flags().StringVarP(&tgzTempDir, "tgz-temp-dir", "", "", "Temporary location for the backup tgz file")
	BackupCmd.Flags().StringVarP(&s3AccessKey, "s3-access-key", "", "", "S3 access key")
	BackupCmd.Flags().StringVarP(&s3SecretKey, "s3-secret-key", "", "", "S3 secret key")
	BackupCmd.Flags().StringVarP(&s3Bucket, "s3-bucket", "", "", "S3 bucket")
	BackupCmd.Flags().StringVarP(&s3Endpoint, "s3-endpoint", "", "", "S3 endpoint")
}
