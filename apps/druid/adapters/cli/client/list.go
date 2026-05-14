package client

import "github.com/spf13/cobra"

var ListCommand = &cobra.Command{
	Use:   "list",
	Short: "List scrolls and status from the daemon",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		scrolls, err := daemon.ListScrolls(cmd.Context())
		if err != nil {
			return err
		}
		return printScrolls(scrolls)
	},
}
