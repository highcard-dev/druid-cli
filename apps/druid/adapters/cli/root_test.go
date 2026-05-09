package cli

import "testing"

func TestRootCommandDoesNotExposeOCICommands(t *testing.T) {
	for _, name := range []string{"pull", "push", "login"} {
		if cmd, _, err := RootCmd.Find([]string{name}); err == nil && cmd != nil && cmd.Name() == name {
			t.Fatalf("druid should not expose %q", name)
		}
	}
}

func TestServeCommandIsSocketOnly(t *testing.T) {
	for _, name := range []string{"tcp", "port"} {
		if flag := ServeCommand.Flags().Lookup(name); flag != nil {
			t.Fatalf("druid serve should not expose --%s", name)
		}
	}
	if flag := ServeCommand.Flags().Lookup("socket"); flag == nil {
		t.Fatal("druid serve should expose --socket")
	}
}

func TestRootCommandDoesNotExposeCWDFlag(t *testing.T) {
	if flag := RootCmd.PersistentFlags().Lookup("cwd"); flag != nil {
		t.Fatal("druid should not expose --cwd")
	}
}
