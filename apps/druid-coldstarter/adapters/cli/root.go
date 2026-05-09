package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/highcard-dev/daemon/apps/druid-coldstarter/adapters/filesystem"
	"github.com/highcard-dev/daemon/apps/druid-coldstarter/core/services"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	var scrollRoot string
	var statusFile string
	var runtimeConfig string

	cmd := &cobra.Command{
		Use:   "druid-coldstarter",
		Short: "Run the standalone Druid coldstart gate",
		RunE: func(cmd *cobra.Command, args []string) error {
			if scrollRoot == "" && runtimeConfig == "" {
				return fmt.Errorf("--scroll-root or --runtime-config is required")
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
			defer stop()
			if runtimeConfig != "" {
				return services.NewColdstarterService(filesystem.NewStatusWriter()).RunWithRuntimeConfig(ctx, runtimeConfig, statusFile)
			}
			return services.NewColdstarterService(filesystem.NewStatusWriter()).Run(ctx, scrollRoot, statusFile)
		},
	}
	cmd.Flags().StringVar(&scrollRoot, "scroll-root", "", "Mounted scroll root containing scroll.yaml")
	cmd.Flags().StringVar(&runtimeConfig, "runtime-config", "", "Generated runtime config path")
	cmd.Flags().StringVar(&statusFile, "status-file", "", "Optional status file path, relative to scroll root unless absolute")
	return cmd
}
