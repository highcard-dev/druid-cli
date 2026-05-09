package cli

import (
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
)

func (a *App) pullCmd() *cobra.Command {
	var noData bool
	cmd := &cobra.Command{
		Use:   "pull <artifact> [dir]",
		Short: "Pull a scroll from an OCI registry (tag or digest)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			artifact := args[0]
			dir := currentWorkingDir()
			if len(args) == 2 {
				dir = args[1]
			}

			registryClient := registry.NewOciClient(a.loadRegistryStore())

			err := registryClient.PullSelective(dir, artifact, !noData, nil)
			if err != nil {
				logger.Log().Error("Failed to pull from registry")
				return err
			}

			logger.Log().Info("Pulled from registry")
			return nil
		},
	}
	cmd.Flags().BoolVar(&noData, "no-data", false, "Skip scroll data files")
	return cmd
}
