package cmd

import (
	"github.com/spf13/cobra"
)

var ScrollCmd = &cobra.Command{
	Use:   "scroll",
	Short: "Scroll tools",
	Args:  cobra.ExactArgs(1),
	Long:  `Scroll tools`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Usage()
	},
}

func init() {
	ScrollCmd.AddCommand(ScrollAppVersionCmd)
}
