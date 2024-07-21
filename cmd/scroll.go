package cmd

import (
	"github.com/spf13/cobra"
)

// scrollCmd represents the command for scrolling
var ScrollCmd = &cobra.Command{
	Use:   "scroll",
	Short: "Commands related to the scroll file",
	Long:  `Commands related to the scroll file`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Usage()
	},
}

func init() {
	ScrollCmd.AddCommand(ScrollValidateCmd)
}
