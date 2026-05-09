package cli

import "github.com/spf13/cobra"

func (a *App) portsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ports <name>",
		Short: "Show runtime port status for a scroll",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.runtimeService()
			if err != nil {
				return err
			}
			ports, err := service.Ports(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(ports)
		},
	}
}
