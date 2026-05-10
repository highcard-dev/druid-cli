package cli

import (
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func (a *App) pushCategoryCmd() *cobra.Command {
	var pushCategoryNamePattern string

	cmd := &cobra.Command{
		Use:   "category",
		Short: "Push locale markdown files (e.g. de-DE.md) from a scroll directory as separate OCI layers.",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			credStore := a.loadRegistryStore()

			repo := args[0]
			category := args[1]
			scrollDir := currentWorkingDir()
			if len(args) == 3 {
				scrollDir = args[2]
			}

			logger.Log().Info("Pushing "+repo+" category to registry", zap.String("scrollDir", scrollDir))

			ociClient := registry.NewOciClient(credStore)

			_, err := ociClient.PushCategory(scrollDir, repo, category)

			if err != nil {
				return err
			}

			logger.Log().Info("Pushed " + repo + " category to registry")
			return nil
		},
	}
	cmd.Flags().StringVar(&pushCategoryNamePattern, "match", "", "Regexp matching file basenames to push (default: locale markdown like de-DE.md)")
	return cmd
}
