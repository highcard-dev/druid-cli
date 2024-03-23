package cmd

import (
	"fmt"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var RunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run single command",
	Args:  cobra.ExactArgs(1),
	Long:  `This command runs a single command from the scroll file.`,
	RunE: func(cmd *cobra.Command, args []string) error {

		host := viper.GetString("registry.host")
		user := viper.GetString("registry.user")
		password := viper.GetString("registry.password")

		client := registry.NewOciClient(host, user, password)

		logManager := services.NewLogManager()
		consoleService := services.NewConsoleManager(logManager)
		processManager := services.NewProcessManager(logManager, consoleService, services.NewProcessMonitor())
		scrollService := services.NewScrollService(cwd)
		processLauncher := services.NewProcessLauncher(client, processManager, services.NewPluginManager(), consoleService, logManager, scrollService)

		if !scrollService.LockExists() {
			scrollService.WriteNewScrollLock()
			logger.Log().Info("Lock file created")
		}

		_, _, err := scrollService.Bootstrap(ignoreVersionCheck)

		if err != nil {
			return fmt.Errorf("error loading scroll: %w", err)
		}

		err = scrollService.RenderCwdTemplates()
		if err != nil {
			return err
		}

		err = processLauncher.LaunchPlugins()

		if err != nil {
			return err
		}

		parts := strings.Split(args[0], ".")

		command := strings.TrimPrefix(args[0], parts[0]+".")

		err = processLauncher.RunNew(command, parts[0], false)
		return err
	},
}

func init() {
	RunCmd.Flags().BoolVarP(&ignoreVersionCheck, "ignore-version-check", "", false, "Ignore version check")
}
