package main

import (
	"fmt"
	"os"

	"github.com/highcard-dev/daemon/apps/druid-dev/adapters/cli"
	"github.com/highcard-dev/daemon/internal/utils/logger"
)

func main() {
	logger.Log(logger.WithStructuredLogging())
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
