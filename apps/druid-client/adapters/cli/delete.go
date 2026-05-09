package cli

import "github.com/spf13/cobra"

func (a *App) deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a scroll from the daemon",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.runtimeService()
			if err != nil {
				return err
			}
			deleted, err := service.Delete(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(deleted)
		},
	}
}
