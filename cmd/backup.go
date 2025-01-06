package cmd

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/services"
	"github.com/spf13/cobra"
)

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

		return snapshotService.Snapshot(scrollService.GetCwd(), destination)
	},
}

func init() {
}
