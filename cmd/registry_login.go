package cmd

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var registryHost string
var registryUser string
var registryPassword string

var LoginCommand = &cobra.Command{
	Use:   "login",
	Short: "Login to OCI registry",
	Long: `Add or update registry credentials in the configuration.
Supports multiple registries with path-based credential matching.

Examples:
  druid registry login --host registry-1.docker.io -u user -p pass
  druid registry login --host artifacts.druid.gg/project1 -u user1 -p pass1
  druid registry login --host artifacts.druid.gg/project2 -u user2 -p pass2`,
	RunE: func(cmd *cobra.Command, args []string) error {

		if err := registry.ValidateCredentials(registryHost, registryUser, registryPassword); err != nil {
			return fmt.Errorf("login failed: %w", err)
		}

		cmd.Println("Login succeeded")

		var registries []domain.RegistryCredential
		viper.UnmarshalKey("registries", &registries)

		newCred := domain.RegistryCredential{
			Host:     registryHost,
			Username: registryUser,
			Password: registryPassword,
		}

		found := false
		for i := range registries {
			if registries[i].Host == registryHost {
				registries[i] = newCred
				found = true
				break
			}
		}

		if !found {
			registries = append(registries, newCred)
		}

		viper.Set("registries", registries)

		return viper.WriteConfig()
	},
}

func init() {
	LoginCommand.Flags().StringVarP(&registryHost, "host", "", "", "OCI registry host (e.g., artifacts.druid.gg/project1)")
	LoginCommand.Flags().StringVarP(&registryUser, "user", "u", "", "username")
	LoginCommand.Flags().StringVarP(&registryPassword, "password", "p", "", "User password")

	LoginCommand.MarkFlagRequired("host")
	LoginCommand.MarkFlagRequired("user")
	LoginCommand.MarkFlagRequired("password")
}
