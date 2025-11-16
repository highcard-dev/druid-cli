package services

import (
	"testing"
)

func TestEnsureNixInstalled(t *testing.T) {
	svc := NewNixDependencyService()
	err := svc.EnsureNixInstalled()
	// We accept either outcome because CI/dev environment may or may not have nix.
	if err != nil {
		t.Logf("nix-shell not found (acceptable if Nix not installed): %v", err)
	}
}

func TestGetCommandPassthrough(t *testing.T) {
	svc := NewNixDependencyService()
	in := []string{"echo", "hello"}
	deps := []string{"nodejs", "python3"}
	out := svc.GetCommand(in, deps)
	expected := []string{"nix-shell", "--pure", "-p", "nodejs", "-p", "python3", "--command", "echo hello"}
	for i, v := range expected {
		if out[i] != v {
			t.Errorf("expected out[%d] = %s got %s", i, v, out[i])
		}
	}
}
