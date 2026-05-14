package client

import "github.com/spf13/cobra"

var StartCommand = &cobra.Command{
	Use:   "start <name>",
	Short: "Start the daemon-managed scroll serve command",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		scroll, err := daemon.StartScroll(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return printJSON(scroll)
	},
}
