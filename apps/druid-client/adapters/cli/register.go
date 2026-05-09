package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func (a *App) registerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register [dir] [name]",
		Short: "Register an already checked-out scroll with the daemon",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := currentWorkingDir()
			name := ""
			if len(args) >= 1 {
				dir = args[0]
			}
			if len(args) == 2 {
				name = args[1]
			}
			scrollRoot, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			info, err := os.Stat(filepath.Join(scrollRoot, "scroll.yaml"))
			if err != nil {
				return fmt.Errorf("registered scroll directory must contain scroll.yaml: %w", err)
			}
			if info.IsDir() {
				return fmt.Errorf("registered scroll directory must contain scroll.yaml file")
			}
			service, err := a.runtimeService()
			if err != nil {
				return err
			}
			scroll, err := service.Create(cmd.Context(), name, scrollRoot, scrollRoot, scrollRoot)
			if err != nil {
				return err
			}
			return printJSON(scroll)
		},
	}
	return cmd
}
