package client

import "github.com/spf13/cobra"

var RoutingTargetsCommand = &cobra.Command{
	Use:   "targets <name>",
	Short: "Show backend service targets for a scroll",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		targets, err := daemon.GetScrollRoutingTargets(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return printJSON(targets)
	},
}
