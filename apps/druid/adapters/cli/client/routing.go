package client

import "github.com/spf13/cobra"

var RoutingCommand = &cobra.Command{
	Use:   "routing",
	Short: "Inspect or apply daemon runtime routing",
}
