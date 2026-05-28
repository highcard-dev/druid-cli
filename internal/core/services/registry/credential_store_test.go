package registry

import (
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func TestCredentialForRepoNormalizesRegistryHost(t *testing.T) {
	store := NewCredentialStore([]domain.RegistryCredential{{
		Host:     "https://artifacts.druid.gg/",
		Username: "robot",
		Password: "secret",
	}})

	cred, err := store.CredentialForRepo("artifacts.druid.gg/user/scroll")
	if err != nil {
		t.Fatalf("CredentialForRepo returned error: %v", err)
	}
	if cred.Username != "robot" || cred.Password != "secret" {
		t.Fatalf("CredentialForRepo returned %#v, want configured credential", cred)
	}
}

func TestCredentialForRepoUsesMostSpecificNormalizedMatch(t *testing.T) {
	store := NewCredentialStore([]domain.RegistryCredential{
		{Host: "artifacts.druid.gg", Username: "generic", Password: "generic-secret"},
		{Host: "https://artifacts.druid.gg/user", Username: "specific", Password: "specific-secret"},
	})

	cred, err := store.CredentialForRepo("https://artifacts.druid.gg/user/scroll")
	if err != nil {
		t.Fatalf("CredentialForRepo returned error: %v", err)
	}
	if cred.Username != "specific" || cred.Password != "specific-secret" {
		t.Fatalf("CredentialForRepo returned %#v, want most specific credential", cred)
	}
}
