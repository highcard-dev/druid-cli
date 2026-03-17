package cmd

import (
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
)

var noData bool

var PullCommand = &cobra.Command{
	Use:   "pull",
	Short: "Pull a scroll from an OCI registry (tag or digest)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		artifact := args[0]

		registryClient := registry.NewOciClient(LoadRegistryStore())

		err := registryClient.PullSelective(cwd, artifact, !noData, nil)
		if err != nil {
			logger.Log().Error("Failed to pull from registry")
			return err
		}

		logger.Log().Info("Pulled from registry")
		return nil
	},
}

func init() {
	PullCommand.Flags().BoolVarP(&noData, "no-data", "", false, "Download full scroll with data files")
}
