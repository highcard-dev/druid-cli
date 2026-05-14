package cli

import "github.com/spf13/cobra"

var WorkerCommand = &cobra.Command{
	Use:   "worker",
	Short: "Run internal Druid worker actions",
}

func init() {
	RootCmd.AddCommand(WorkerCommand)
}
