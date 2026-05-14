package cli

import (
	"strings"
	"testing"
)

func TestRootCommandHasNoRuntimeFlags(t *testing.T) {
	cmd := NewRootCommand()
	if cmd.Flags().Lookup("root") != nil {
		t.Fatal("did not expect root flag")
	}
	if cmd.Flags().Lookup("status-file") != nil {
		t.Fatal("did not expect status-file flag")
	}
	if cmd.Flags().Lookup("scroll-root") != nil {
		t.Fatal("did not expect scroll-root flag")
	}
	if cmd.Flags().Lookup("runtime-config") != nil {
		t.Fatal("did not expect runtime-config flag")
	}
}

func TestRootCommandRequiresRootEnv(t *testing.T) {
	t.Setenv(rootEnv, "")
	cmd := NewRootCommand()
	cmd.SetArgs(nil)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), rootEnv+" is required") {
		t.Fatalf("expected %s validation error, got %v", rootEnv, err)
	}
}
