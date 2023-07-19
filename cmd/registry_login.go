package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var registryHost string
var registryUser string
var registryPassword string

var LoginCommand = &cobra.Command{
	Use:   "login",
	Short: "Login to OCI registry",
	RunE: func(cmd *cobra.Command, args []string) error {

		viper.Set("registry.host", registryHost)
		viper.Set("registry.user", registryUser)
		viper.Set("registry.password", registryPassword)

		//TODO: validate credentials

		err := viper.WriteConfig()

		return err
	},
}

func init() {

	LoginCommand.Flags().StringVarP(&registryHost, "host", "", "", "OCI registry host")

	LoginCommand.Flags().StringVarP(&registryUser, "user", "u", "", "username")

	LoginCommand.Flags().StringVarP(&registryPassword, "password", "p", "", "User password")

	LoginCommand.MarkFlagRequired("host")
	LoginCommand.MarkFlagRequired("user")
	LoginCommand.MarkFlagRequired("password")
}
