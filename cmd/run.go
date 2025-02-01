package cmd

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	snapshotService "github.com/highcard-dev/daemon/internal/core/services/snapshot"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var RunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run single command",
	Args:  cobra.ExactArgs(1),
	Long:  `This command runs a single command from the scroll file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		command := args[0]

		host := viper.GetString("registry.host")
		user := viper.GetString("registry.user")
		password := viper.GetString("registry.password")

		client := registry.NewOciClient(host, user, password)

		ctx := cmd.Context()

		disablePrometheus, ok := ctx.Value("disablePrometheus").(bool)

		//only disable prometheus if context value is set and true
		processMonitor := services.NewProcessMonitor(!ok || !disablePrometheus)

		logManager := services.NewLogManager()
		consoleService := services.NewConsoleManager(logManager)
		processManager := services.NewProcessManager(logManager, consoleService, processMonitor)
		scrollService, err := services.NewScrollService(cwd)
		if err != nil {
			return fmt.Errorf("error creating scroll service: %w", err)
		}
		processLauncher := services.NewProcedureLauncher(client, processManager, services.NewPluginManager(), consoleService, logManager, scrollService)

		queueManager := services.NewQueueManager(scrollService, processLauncher)
		snapshotService := snapshotService.NewSnapshotService()

		_, err = initScroll(scrollService, snapshotService, processLauncher, queueManager)

		logger.Log().Info("Adding command to queue", zap.String("command", command))
		err = queueManager.AddTempItem(command)
		if err != nil {
			return err
		}

		queueManager.WaitUntilEmpty()

		return nil
	},
}

func init() {
	RunCmd.Flags().BoolVarP(&ignoreVersionCheck, "ignore-version-check", "", false, "Ignore version check")
}
