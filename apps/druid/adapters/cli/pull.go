package cli

import (
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
)

var pullNoData bool

var PullCommand = &cobra.Command{
	Use:   "pull <artifact> [dir]",
	Short: "Pull a scroll from an OCI registry (tag or digest)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		artifact := args[0]
		dir := currentWorkingDir()
		if len(args) == 2 {
			dir = args[1]
		}

		registryClient := registry.NewOciClient(loadRegistryStore())

		err := registryClient.PullSelective(dir, artifact, !pullNoData, nil)
		if err != nil {
			logger.Log().Error("Failed to pull from registry")
			return err
		}

		logger.Log().Info("Pulled from registry")
		return nil
	},
}

func init() {
	RootCmd.AddCommand(PullCommand)
	PullCommand.Flags().BoolVar(&pullNoData, "no-data", false, "Skip scroll data files")
}
