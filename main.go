package main

// @title           Druid CLI
// @version         0.1.0
// @description     Druid CLI is a process runner to launches and manages various sorts of applications, like gameservers, databases or webservers.

import (
	"os"

	"github.com/highcard-dev/daemon/cmd"
	"github.com/highcard-dev/daemon/internal/utils/logger"
)

func main() {
	logger.Log(logger.WithStructuredLogging())
	//druid start
	//druid run [update]
	if err := cmd.RootCmd.Execute(); err != nil {
		// log it, then
		os.Exit(23)
	}

}
