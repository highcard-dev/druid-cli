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
	Use:   "coldstarter",
	Short: "Starts the coldstarter only",
	Long:  "Starts the coldstarter only and waits to finish",
	RunE: func(cmd *cobra.Command, args []string) error {

		scrollService, err := services.NewScrollService(cwd)
		if err != nil {
			return fmt.Errorf("failed to load scroll - %w", err)
		}

		currentScroll := scrollService.GetCurrent()

		if len(currentScroll.Ports) == 0 {
			return fmt.Errorf("no ports found in scroll")
		}

		logger.Log().Info("Scroll loaded", zap.String("Name", currentScroll.Name), zap.Any("Version", currentScroll.Version), zap.String("AppVersion", currentScroll.AppVersion), zap.Any("Ports", currentScroll.Ports))

		portsService := services.NewPortServiceWithScrollFile(&currentScroll.File)

		coldStarter := services.NewColdStarter(scrollService.GetDir(), portsService.GetPorts())

		coldStarter.StartOnce(context.TODO())
		logger.Log().Info("Coldstarter finished")
		return nil
	},
}

func init() {
}
