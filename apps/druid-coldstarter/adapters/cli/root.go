package cli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/highcard-dev/daemon/apps/druid-coldstarter/adapters/filesystem"
	"github.com/highcard-dev/daemon/apps/druid-coldstarter/core/services"
	"github.com/spf13/cobra"
)

const (
	rootEnv       = "DRUID_ROOT"
	statusFileEnv = "DRUID_COLDSTARTER_STATUS_FILE"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "druid-coldstarter",
		Short: "Run the standalone Druid coldstart gate",
		RunE: func(cmd *cobra.Command, args []string) error {
			root := os.Getenv(rootEnv)
			if root == "" {
				return errors.New(rootEnv + " is required")
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
			defer stop()
			return services.NewColdstarterService(filesystem.NewStatusWriter()).Run(ctx, root, os.Getenv(statusFileEnv))
		},
	}
	cmd.SilenceUsage = true
	return cmd
}
