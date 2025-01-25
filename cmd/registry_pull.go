package cmd

import (
	"path/filepath"

	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var PullCommand = &cobra.Command{
	Use:   "pull",
	Short: "Generate OCI Artifacts and push to a remote registry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		artifact := args[0]

		user := viper.GetString("registry.user")
		password := viper.GetString("registry.password")
		host := viper.GetString("registry.host")

		registryClient := registry.NewOciClient(host, user, password)

		err := registryClient.Pull(filepath.Join(cwd, ".scroll"), artifact)
		if err != nil {
			logger.Log().Error("Failed to pull from registry")
			return err
		}

		logger.Log().Info("Pulled from registry")
		return nil
	},
}

func init() {
}
