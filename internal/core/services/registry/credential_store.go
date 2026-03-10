package registry

import (
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"oras.land/oras-go/v2/registry/remote/auth"
)

type CredentialStore struct {
	registries []domain.RegistryCredential
}

func NewCredentialStore(registries []domain.RegistryCredential) *CredentialStore {
	return &CredentialStore{registries: registries}
}

func (s *CredentialStore) CredentialForRepo(repoURL string) (auth.Credential, error) {
	repoURL = strings.TrimPrefix(repoURL, "https://")
	repoURL = strings.TrimPrefix(repoURL, "http://")

	var bestMatch *domain.RegistryCredential
	bestLen := 0

	for i := range s.registries {
		reg := &s.registries[i]
		if strings.HasPrefix(repoURL, reg.Host) && len(reg.Host) > bestLen {
			bestMatch = reg
			bestLen = len(reg.Host)
		}
	}

	if bestMatch == nil || bestMatch.Username == "" || bestMatch.Password == "" {
		return auth.EmptyCredential, nil
	}

	return auth.Credential{
		Username: bestMatch.Username,
		Password: bestMatch.Password,
	}, nil
}

func (s *CredentialStore) HasCredentials() bool {
	return len(s.registries) > 0
}
