package client

import "github.com/spf13/cobra"

var UpdateCommand = &cobra.Command{
	Use:   "update <name> [artifact]",
	Short: "Update a daemon-managed scroll runtime",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		artifact := ""
		if len(args) == 2 {
			artifact = args[1]
		}
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		scroll, err := daemon.UpdateScroll(cmd.Context(), args[0], artifact, registryCredentials())
		if err != nil {
			return err
		}
		return printJSON(scroll)
	},
}
