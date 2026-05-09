package main

import (
	"fmt"
	"os"

	"github.com/highcard-dev/daemon/apps/druid-coldstarter/adapters/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
