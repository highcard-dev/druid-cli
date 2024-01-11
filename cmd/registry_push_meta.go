package cmd

import (
	"fmt"
	"path"

	logger "github.com/highcard-dev/daemon/internal/core/services/log"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var PushMetaCommand = &cobra.Command{
	Use:   "meta",
	Short: "Generate Meta OCI Artifacts and push to a remote registry.",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {

		user := viper.GetString("registry.user")
		password := viper.GetString("registry.password")
		host := viper.GetString("registry.host")

		if user == "" || password == "" || host == "" {
			return fmt.Errorf("registry host, user and password must be set. Please use `druid registry login` to set them")
		}

		artifact := args[0]
		folder := "."
		if len(args) == 2 {
			folder = args[1]
		}

		fullPath := path.Join(cwd, folder)

		ociClient := registry.NewOciClient(host, user, password)

		_, err := ociClient.PushMeta(fullPath, artifact)

		if err != nil {
			return err
		}

		logger.Log().Info("Pushed to registry")
		return nil
	},
}
