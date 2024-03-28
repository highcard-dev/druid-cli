package main

import (
	"os"

	"github.com/highcard-dev/daemon/cmd"
	"github.com/highcard-dev/daemon/internal/utils/logger"
)

func main() {
	logger.Log(logger.WithStructuredLogging())
	//druidd start
	//druidd run [update]
	cmd.RootCmd.AddCommand(cmd.ServeCommand)
	cmd.RootCmd.AddCommand(cmd.RunCmd)
	cmd.RootCmd.AddCommand(cmd.RegistryCmd)
	cmd.RootCmd.AddCommand(cmd.UpdateCommand)
	cmd.RootCmd.AddCommand(cmd.SemverCmd)
	cmd.RootCmd.AddCommand(cmd.VersionCmd)
	if err := cmd.RootCmd.Execute(); err != nil {
		// log it, then
		os.Exit(23)
	}

}
