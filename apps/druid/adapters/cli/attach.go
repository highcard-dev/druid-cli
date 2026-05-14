package cli

import (
	"github.com/highcard-dev/daemon/apps/druid/adapters/websocketclient"
	"github.com/spf13/cobra"
)

var AttachCommand = &cobra.Command{
	Use:   "attach <name> <console>",
	Short: "Attach to a daemon-managed runtime console",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return websocketclient.NewAttacher(daemonSocket).Attach(cmd.Context(), args[0], args[1])
	},
}

func init() {
	RootCmd.AddCommand(AttachCommand)
}
