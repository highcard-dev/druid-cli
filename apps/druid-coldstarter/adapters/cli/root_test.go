package cli

import (
	"strings"
	"testing"
)

func TestRootCommandHasNoRuntimeFlags(t *testing.T) {
	cmd := NewRootCommand()
	for _, flag := range []string{"root", "scroll" + "-" + "root", "runtime" + "-" + "config", "status" + "-" + "file"} {
		if cmd.Flags().Lookup(flag) != nil {
			t.Fatalf("did not expect %s flag", flag)
		}
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
