package cli

import "github.com/spf13/cobra"

func (a *App) startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <name>",
		Short: "Start the daemon-managed scroll serve command",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.runtimeService()
			if err != nil {
				return err
			}
			scroll, err := service.Start(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(scroll)
		},
	}
}

func (a *App) stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop daemon-managed runtime workloads for a scroll",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.runtimeService()
			if err != nil {
				return err
			}
			scroll, err := service.Stop(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(scroll)
		},
	}
}
