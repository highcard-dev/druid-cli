package cmd

import (
	"fmt"
	"path"

	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var pushCategoryNamePattern string

// druid push category <repo> <category> <scrollDir>
var PushCategoryCommand = &cobra.Command{
	Use:   "category",
	Short: "Push locale markdown files (e.g. de-DE.md) from a scroll directory as separate OCI layers.",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {

		credStore := LoadRegistryStore()
		if !credStore.HasCredentials() {
			return fmt.Errorf("no registry credentials configured. Please use `druid registry login` to set them")
		}

		repo := args[0]
		category := args[1]
		scrollDir := "."
		if len(args) == 3 {
			scrollDir = args[2]
		}

		fullPath := path.Join(cwd, scrollDir)

		logger.Log().Info("Pushing "+repo+" category to registry", zap.String("scrollDir", fullPath))

		ociClient := registry.NewOciClient(credStore)

		_, err := ociClient.PushCategory(fullPath, repo, category)

		if err != nil {
			return err
		}

		logger.Log().Info("Pushed " + repo + " category to registry")
		return nil
	},
}

func init() {
	PushCategoryCommand.Flags().StringVar(&pushCategoryNamePattern, "match", "", "Regexp matching file basenames to push (default: locale markdown like de-DE.md)")
}
