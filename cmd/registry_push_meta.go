package cmd

import (
	"fmt"
	"path"

	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var PushMetaCommand = &cobra.Command{
	Use:   "meta",
	Short: "Generate Meta OCI Artifacts and push to a remote registry.",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {

		credStore := LoadRegistryStore()
		if !credStore.HasCredentials() {
			return fmt.Errorf("no registry credentials configured. Please use `druid registry login` to set them")
		}

		artifact := args[0]
		folder := "."
		if len(args) == 2 {
			folder = args[1]
		}

		fullPath := path.Join(cwd, folder)

		logger.Log().Info("Pushing "+artifact+" meta to registry", zap.String("path", fullPath))

		ociClient := registry.NewOciClient(credStore)

		_, err := ociClient.PushMeta(fullPath, artifact)

		if err != nil {
			return err
		}

		logger.Log().Info("Pushed " + artifact + " meta to registry")
		return nil
	},
}
