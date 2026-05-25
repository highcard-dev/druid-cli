package client

import "github.com/spf13/cobra"

var DeleteCommand = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a scroll from the daemon",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		deleted, err := daemon.DeleteScroll(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return printJSON(deleted)
	},
}
