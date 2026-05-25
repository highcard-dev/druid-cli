package client

import (
	"context"

	"github.com/highcard-dev/daemon/internal/api"
	"github.com/spf13/cobra"
)

var CreateCommand = &cobra.Command{
	Use:   "create <artifact-or-path> [name]",
	Short: "Create a scroll through the daemon",
	Example: `  druid create ./scroll my-scroll -p 8080:http
  druid create artifacts.example/app:v1 my-scroll -p 8080:80
  druid create ./scroll my-scroll -p 127.0.0.1:8080:http
  druid create ./scroll my-scroll -p 8443:http/https`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		artifact := args[0]
		name := ""
		if len(args) == 2 {
			name = args[1]
		}
		runtimeClient, err := runtimeDaemonClient()
		if err != nil {
			return err
		}

		scroll, err := createScrollWithRouting(cmd.Context(), runtimeClient, artifact, name, registryCredentials(), createPublishes)
		if err != nil {
			return err
		}
		return printJSON(scroll)
	},
}

var createPublishes []string

func init() {
	CreateCommand.Flags().StringArrayVarP(&createPublishes, "publish", "p", nil, "Publish routing as [external-ip:]public-port:target[/protocol]")
}

func createScrollWithRouting(ctx context.Context, daemon RuntimeDaemon, artifact string, name string, registryCredentials []api.RegistryCredential, publishes []string) (*api.RuntimeScroll, error) {
	scroll, err := daemon.CreateScroll(ctx, name, artifact, registryCredentials)
	if err != nil {
		return nil, err
	}
	if len(publishes) == 0 {
		return scroll, nil
	}
	return applyPublishedRouting(ctx, daemon, scroll.Id, publishes)
}
