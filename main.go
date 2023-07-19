package main

import (
	"os"

	"github.com/highcard-dev/daemon/cmd"
)

var currentWorkingDirectory string

func main() {
	//druidd start
	//druidd run [update]
	cmd.RootCmd.AddCommand(cmd.ServeCommand)
	cmd.RootCmd.AddCommand(cmd.RunCmd)
	cmd.RootCmd.AddCommand(cmd.RegistryCmd)
	cmd.RootCmd.AddCommand(cmd.UpdateCommand)
	if err := cmd.RootCmd.Execute(); err != nil {
		// log it, then
		os.Exit(23)
	}

}
