package cli

import (
	ws "github.com/highcard-dev/daemon/apps/druid-client/adapters/websocket"
	"github.com/spf13/cobra"
)

func (a *App) attachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <name> <console>",
		Short: "Attach to a daemon-managed runtime console",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return ws.NewAttacher(a.daemonSocket).Attach(cmd.Context(), args[0], args[1])
		},
	}
}
