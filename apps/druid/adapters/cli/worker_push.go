package cli

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/spf13/cobra"
)

var workerPushArtifact string
var workerPushRoot string
var workerPushPreserveReleaseManifest bool

var WorkerPushCommand = &cobra.Command{
	Use:   "push",
	Short: "Push a mounted runtime root as an OCI artifact",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if workerPushArtifact == "" {
			return fmt.Errorf("artifact is required")
		}
		if workerPushRoot == "" {
			workerPushRoot = "/scroll"
		}
		scroll, err := domain.NewScroll(workerPushRoot)
		if err != nil {
			return err
		}
		repo, tag := utils.SplitArtifact(workerPushArtifact)
		oci := registry.NewOciClient(loadWorkerRegistryStore())
		_, err = oci.PushWithOptions(workerPushRoot, repo, tag, nil, false, &scroll.File, registry.TransferOptions{PreserveReleaseManifest: workerPushPreserveReleaseManifest})
		return err
	},
}

func init() {
	WorkerCommand.AddCommand(WorkerPushCommand)
	WorkerPushCommand.Flags().StringVar(&workerPushArtifact, "artifact", "", "OCI artifact to push")
	WorkerPushCommand.Flags().StringVar(&workerPushRoot, "root", "/scroll", "Mounted runtime root path")
	WorkerPushCommand.Flags().BoolVar(&workerPushPreserveReleaseManifest, "preserve-release-manifest", false, "Include the installed release manifest.json in the backup payload")
	WorkerPushCommand.MarkFlagRequired("artifact")
}
