package main

import (
	"os"

	"github.com/highcard-dev/daemon/apps/druid-client/adapters/cli"
	"github.com/highcard-dev/daemon/internal/utils/logger"
)

func main() {
	logger.Log(logger.WithStructuredLogging())
	if err := cli.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
