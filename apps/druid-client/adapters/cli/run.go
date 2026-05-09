package cli

import "github.com/spf13/cobra"

func (a *App) runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <name> <command>",
		Short: "Run a command on a daemon-managed scroll",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.runtimeService()
			if err != nil {
				return err
			}
			scroll, err := service.Run(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			return printJSON(scroll)
		},
	}
}
