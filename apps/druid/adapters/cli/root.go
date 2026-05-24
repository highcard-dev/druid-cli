package cli

import (
	"context"
	"os"

	"github.com/highcard-dev/daemon/apps/druid/adapters/cli/client"
	"github.com/highcard-dev/daemon/apps/druid/adapters/daemonclient"
	"github.com/highcard-dev/daemon/apps/druid/adapters/websocketclient"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var envPath string
var configFile string
var runtimeStateDir string
var runtimeBackendName string
var daemonSocket string
var daemonURL string

var RootCmd = &cobra.Command{
	Use:   "druid",
	Short: "Druid Daemon that enable Scroll usage and communication",
	Long: `A Scalable and Customizable daemon
               to run any of your applications with the usage of Scrolls.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Usage()
	},
}

func init() {
	cobra.OnInitialize(initConfig)

	RootCmd.PersistentFlags().StringVarP(&envPath, "env-file", "e", "./.env", "Path to environment file (.env)")
	RootCmd.PersistentFlags().StringVar(&configFile, "config", "", "Path to config file (default: ~/.druid.yaml)")
	RootCmd.PersistentFlags().StringVar(&daemonSocket, "daemon-socket", utils.DefaultRuntimeSocketPath(), "Runtime daemon Unix socket path for REST-backed commands")
	RootCmd.PersistentFlags().StringVar(&daemonURL, "daemon-url", "", "Runtime daemon HTTP URL for REST-backed commands")

	client.Register(RootCmd, client.Config{
		Daemon: func() (client.RuntimeDaemon, error) {
			return daemonclient.NewOpenAPIClientForTarget(daemonSocket, daemonURL)
		},
		AttachConsole: func(ctx context.Context, scroll string, console string) error {
			return websocketclient.NewAttacherForTarget(daemonSocket, daemonURL).Attach(ctx, scroll, console)
		},
		RegistryCredentials: func() []api.RegistryCredential {
			return client.RegistryCredentials(loadRegistryStore().Credentials())
		},
	})
}

func initConfig() {
	viper.AutomaticEnv()

	if configFile != "" {
		viper.SetConfigFile(configFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		viper.SetConfigType("yaml")
		viper.SetConfigName(".druid")
		viper.AddConfigPath(home)
	}

	viper.SafeWriteConfig()
	viper.ReadInConfig()
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func currentWorkingDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
