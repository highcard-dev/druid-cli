package cmd

import (
	"fmt"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
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
		hub := services.NewHub()
		processManager := services.NewProcessManager(logManager, hub)
		scrollService := services.NewScrollService(cwd, client, logManager, processManager, hub, services.NewPluginManager())

		_, err := scrollService.Load(ignoreVersionCheck)

		if err != nil {
			return fmt.Errorf("error loading scroll: %w", err)
		}

		parts := strings.Split(args[0], ".")

		command := strings.TrimPrefix(args[0], parts[0]+".")

		err = scrollService.Run(command, parts[0], false)
		return err
	},
}

func init() {
	RunCmd.Flags().BoolVarP(&ignoreVersionCheck, "ignore-version-check", "", false, "Ignore version check")
}
