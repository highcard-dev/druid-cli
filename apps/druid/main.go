package main

import (
	"os"

	"github.com/highcard-dev/daemon/apps/druid/adapters/cli"
	"github.com/highcard-dev/daemon/internal/utils/logger"
)

func main() {
	logger.Log(logger.WithStructuredLogging())
	if err := cli.RootCmd.Execute(); err != nil {
		os.Exit(23)
	}
}
