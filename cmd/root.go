package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var envPath string
var cwd string

var RootCmd = &cobra.Command{
	Use:   "druidd",
	Short: "Druid Daemon that enable Scroll usage and communication",
	Long: `A Scalable and Customizable daemon
               to run any of your applications with the usage of Scrolls.
               Complete documentation is available at https://druid.gg`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Usage()
	},
}

func init() {

	viper.SetDefault("registry.host", "registry-1.docker.io")
	cobra.OnInitialize(initConfig)

	c, _ := os.Getwd()

	RootCmd.PersistentFlags().StringVarP(&cwd, "cwd", "", c, "Path to environment file (.env)")

	RootCmd.PersistentFlags().StringVarP(&envPath, "env-file", "e", "./.env", "Path to environment file (.env)")
	RootCmd.PersistentFlags().BoolVar(&ignoreVersionCheck, "ignore-version-check", false, "Ignore version check between scroll.yaml and scroll-lock.json")

}

func initConfig() {
	home, err := os.UserHomeDir()
	cobra.CheckErr(err)

	viper.SetConfigType("yaml")
	viper.SetConfigName(".druid")

	// Search config in home directory with name ".cobra" (without extension).
	viper.AddConfigPath(home)

	viper.AutomaticEnv()
	viper.SafeWriteConfig()
	viper.ReadInConfig()
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
