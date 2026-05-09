package cli

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var envPath string
var configFile string
var runtimeStateDir string
var runtimeBackend string

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
	RootCmd.AddCommand(UpdateCommand)
	RootCmd.AddCommand(AppVersionCmd)
	RootCmd.AddCommand(VersionCmd)
	RootCmd.AddCommand(ValidateCmd)

	RootCmd.PersistentFlags().StringVarP(&envPath, "env-file", "e", "./.env", "Path to environment file (.env)")
	RootCmd.PersistentFlags().StringVar(&configFile, "config", "", "Path to config file (default: ~/.druid.yaml)")

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
