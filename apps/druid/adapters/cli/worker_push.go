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
		_, err = oci.Push(workerPushRoot, repo, tag, nil, false, &scroll.File)
		return err
	},
}

func init() {
	WorkerCommand.AddCommand(WorkerPushCommand)
	WorkerPushCommand.Flags().StringVar(&workerPushArtifact, "artifact", "", "OCI artifact to push")
	WorkerPushCommand.Flags().StringVar(&workerPushRoot, "root", "/scroll", "Mounted runtime root path")
	WorkerPushCommand.MarkFlagRequired("artifact")
}
