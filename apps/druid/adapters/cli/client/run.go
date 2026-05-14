package client

import "github.com/spf13/cobra"

var RunCommand = &cobra.Command{
	Use:   "run <name> <command>",
	Short: "Run a command on a daemon-managed scroll",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		scroll, err := daemon.RunScrollCommand(cmd.Context(), args[0], args[1])
		if err != nil {
			return err
		}
		return printJSON(scroll)
	},
}
