package cli

import "github.com/spf13/cobra"

func (a *App) describeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "describe <name>",
		Short: "Describe a scroll from the daemon",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.runtimeService()
			if err != nil {
				return err
			}
			scroll, err := service.Describe(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(scroll)
		},
	}
}
