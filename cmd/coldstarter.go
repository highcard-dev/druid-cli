package cmd

import (
	"context"
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var ColdstarterCmd = &cobra.Command{
	Use:   "run",
	Short: "Run single command",
	Args:  cobra.ExactArgs(1),
	Long:  `This command runs a single command from the scroll file.`,
	RunE: func(cmd *cobra.Command, args []string) error {

		scrollService, err := services.NewScrollService(cwd)
		if err != nil {
			return fmt.Errorf("failed to load scroll - %w", err)
		}

		currentScroll := scrollService.GetCurrent()

		logger.Log().Info("Scroll loaded", zap.String("Name", currentScroll.Name), zap.Any("Version", currentScroll.Version), zap.String("AppVersion", currentScroll.AppVersion), zap.Any("Ports", currentScroll.Ports))

		coldStarter := services.NewColdStarter(scrollService.GetDir(), currentScroll.GetColdStartPorts())

		coldStarter.StartOnce(context.TODO())
		logger.Log().Info("Coldstarter finished")
		return nil
	},
}

func init() {
}
