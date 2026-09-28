package client

import "github.com/spf13/cobra"

var UpdateCommand = &cobra.Command{
	Use:   "update <name> <repository@sha256:digest>",
	Short: "Apply an explicitly accepted immutable Scroll revision",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		artifact := args[1]
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
