package cmd

import (
	"github.com/spf13/cobra"
)

var version string

var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print cli version",
	Long:  `This command prints the version of the cli.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println("Version:", version)
		return nil
	},
}

func init() {
}
