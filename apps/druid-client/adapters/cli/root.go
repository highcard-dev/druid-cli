package cli

import (
	"os"

	"github.com/highcard-dev/daemon/apps/druid-client/adapters/daemon"
	"github.com/highcard-dev/daemon/apps/druid-client/core/services"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type App struct {
	daemonSocket string
	configFile   string
	envPath      string
}

func NewRootCommand() *cobra.Command {
	app := &App{}
	cmd := &cobra.Command{
		Use:   "druid-client",
		Short: "Druid runtime daemon client",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Usage()
		},
	}
	cobra.OnInitialize(app.initConfig)
	cmd.PersistentFlags().StringVar(&app.daemonSocket, "daemon-socket", utils.DefaultRuntimeSocketPath(), "Runtime daemon Unix socket path")
	cmd.PersistentFlags().StringVar(&app.configFile, "config", "", "Path to config file (default: ~/.druid.yaml)")
	cmd.PersistentFlags().StringVarP(&app.envPath, "env-file", "e", "./.env", "Path to environment file (.env)")
	cmd.AddCommand(app.createCmd())
	cmd.AddCommand(app.registerCmd())
	cmd.AddCommand(app.listCmd())
	cmd.AddCommand(app.describeCmd())
	cmd.AddCommand(app.deleteCmd())
	cmd.AddCommand(app.runCmd())
	cmd.AddCommand(app.startCmd())
	cmd.AddCommand(app.stopCmd())
	cmd.AddCommand(app.portsCmd())
	cmd.AddCommand(app.routingCmd())
	cmd.AddCommand(app.attachCmd())
	cmd.AddCommand(app.pullCmd())
	cmd.AddCommand(app.pushCmd())
	cmd.AddCommand(app.loginCmd())
	return cmd
}

func (a *App) initConfig() {
	viper.AutomaticEnv()
	if a.configFile != "" {
		viper.SetConfigFile(a.configFile)
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

func (a *App) loadRegistryStore() *registry.CredentialStore {
	var registries []domain.RegistryCredential
	viper.UnmarshalKey("registries", &registries)
	if len(registries) == 0 {
		host := viper.GetString("registry.host")
		user := viper.GetString("registry.user")
		password := viper.GetString("registry.password")
		if host != "" {
			registries = append(registries, domain.RegistryCredential{Host: host, Username: user, Password: password})
		}
	}
	return registry.NewCredentialStore(registries)
}

func (a *App) runtimeService() (*services.RuntimeService, error) {
	client, err := daemon.NewOpenAPIClient(a.daemonSocket)
	if err != nil {
		return nil, err
	}
	return services.NewRuntimeService(client), nil
}

func currentWorkingDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
