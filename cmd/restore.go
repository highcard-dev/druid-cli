package cmd

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/services"
	"github.com/spf13/cobra"
)

var tempDir string
var skipSafety bool

var RestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore the current scroll",
	Long:  "Restore the current scroll, by specifying the s3 bucket to backup to or a pre-signed url",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		source := args[0]

		scrollService, err := services.NewScrollService(cwd)
		if scrollService == nil {
			return fmt.Errorf("failed to load scroll - %w", err)
		}

		snapshotService := services.NewRestoreService()

		err = snapshotService.RestoreSnapshot(scrollService.GetCwd(), source)

		return err
	},
}

func init() {
	RestoreCmd.Flags().StringVarP(&tempDir, "temp-dir", "", "", "Temporary directory where to move the soon to be deleted files to. Files will be restored, in case an error occured while fetching / extracting the snapshot. Only works when --skip-safety is not set.")
	RestoreCmd.Flags().BoolVarP(&skipSafety, "skip-safety", "", false, "Skips the renaming of the soon to be deleted files. If set the original state will NOT be recovered, if an error happens during fetching / extracting of the snapshot.")
}
