package cli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/highcard-dev/daemon/apps/druid-coldstarter/core/services"
	"github.com/spf13/cobra"
)

const (
	rootEnv = "DRUID_ROOT"
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
			return services.NewColdstarterService().Run(ctx, root)
		},
	}
	cmd.SilenceUsage = true
	return cmd
}
