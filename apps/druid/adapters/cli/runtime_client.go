package cli

import (
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/spf13/viper"
)

func loadRegistryStore() *registry.CredentialStore {
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
