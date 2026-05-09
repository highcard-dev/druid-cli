package cli

import "github.com/spf13/cobra"

func (a *App) listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List scrolls and status from the daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.runtimeService()
			if err != nil {
				return err
			}
			scrolls, err := service.List(cmd.Context())
			if err != nil {
				return err
			}
			return printScrolls(scrolls)
		},
	}
}
