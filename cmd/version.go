package cmd

import (
	constants "github.com/highcard-dev/daemon/internal"
	"github.com/spf13/cobra"
)

var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print cli version",
	Long:  `This command prints the version of the cli.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println("Version:", constants.Version)
		return nil
	},
}
