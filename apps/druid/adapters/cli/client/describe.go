package client

import "github.com/spf13/cobra"

var DescribeCommand = &cobra.Command{
	Use:   "describe <name>",
	Short: "Describe a scroll from the daemon",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		scroll, err := daemon.GetScroll(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return printJSON(scroll)
	},
}
