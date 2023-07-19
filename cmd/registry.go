package cmd

import (
	"github.com/spf13/cobra"
)

var RegistryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Druid Scroll Registry Tool",
	Long:  `An application that enable managing scrolls versioning and packaging`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Usage()
	},
}

func init() {

	RegistryCmd.AddCommand(PushCommand)
	RegistryCmd.AddCommand(PullCommand)
	RegistryCmd.AddCommand(LoginCommand)
}
