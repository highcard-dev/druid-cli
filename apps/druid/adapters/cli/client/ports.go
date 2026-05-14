package client

import "github.com/spf13/cobra"

var PortsCommand = &cobra.Command{
	Use:   "ports <name>",
	Short: "Show runtime port status for a scroll",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		ports, err := daemon.GetScrollPorts(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return printJSON(ports)
	},
}
