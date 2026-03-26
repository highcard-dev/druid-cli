package cmd

import (
	"os"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var envPath string
var cwd string
var ignoreLockfileQueue bool
var configFile string

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

	RootCmd.AddCommand(ServeCommand)
	RootCmd.AddCommand(RunCmd)
	RootCmd.AddCommand(RegistryCmd)
	RootCmd.AddCommand(UpdateCommand)
	RootCmd.AddCommand(SemverCmd)
	RootCmd.AddCommand(VersionCmd)
	RootCmd.AddCommand(ScrollCmd)
	RootCmd.AddCommand(PortMonitorCmd)
	RootCmd.AddCommand(ColdstarterCmd)

	c, _ := os.Getwd()

	RootCmd.PersistentFlags().StringVarP(&cwd, "cwd", "", c, "Path to environment file (.env)")

	RootCmd.PersistentFlags().StringVarP(&envPath, "env-file", "e", "./.env", "Path to environment file (.env)")
	RootCmd.PersistentFlags().StringVar(&configFile, "config", "", "Path to config file (default: ~/.druid.yaml)")
	RootCmd.PersistentFlags().BoolVar(&ignoreVersionCheck, "ignore-version-check", false, "Ignore version check between scroll.yaml and scroll-lock.json")
	RootCmd.PersistentFlags().BoolVar(&ignoreLockfileQueue, "ignore-lockfile-queue", false, "Skip queuing the lock file")

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

func LoadRegistryStore() *registry.CredentialStore {
	var registries []domain.RegistryCredential
	viper.UnmarshalKey("registries", &registries)

	if len(registries) == 0 {
		host := viper.GetString("registry.host")
		user := viper.GetString("registry.user")
		password := viper.GetString("registry.password")

		if host != "" {
			registries = append(registries, domain.RegistryCredential{
				Host:     host,
				Username: user,
				Password: password,
			})
		}
	}

	return registry.NewCredentialStore(registries)
}
