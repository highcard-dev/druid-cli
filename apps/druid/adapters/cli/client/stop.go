package client

import "github.com/spf13/cobra"

var StopCommand = &cobra.Command{
	Use:   "stop <name>",
	Short: "Stop daemon-managed runtime workloads for a scroll",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		scroll, err := daemon.StopScroll(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return printJSON(scroll)
	},
}
