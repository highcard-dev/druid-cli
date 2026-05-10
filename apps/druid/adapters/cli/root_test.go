package cli

import "testing"

func TestRootCommandDoesNotExposeOCICommands(t *testing.T) {
	for _, name := range []string{"pull", "push", "login"} {
		if cmd, _, err := RootCmd.Find([]string{name}); err == nil && cmd != nil && cmd.Name() == name {
			t.Fatalf("druid should not expose %q", name)
		}
	}
}

func TestServeCommandExposesRuntimeListeners(t *testing.T) {
	for _, name := range []string{"tcp", "port"} {
		if flag := ServeCommand.Flags().Lookup(name); flag != nil {
			t.Fatalf("druid serve should not expose --%s", name)
		}
	}
	for _, name := range []string{"socket", "listen", "public-listen", "internal-token"} {
		if flag := ServeCommand.Flags().Lookup(name); flag == nil {
			t.Fatalf("druid serve should expose --%s", name)
		}
	}
}

func TestRootCommandDoesNotExposeCWDFlag(t *testing.T) {
	if flag := RootCmd.PersistentFlags().Lookup("cwd"); flag != nil {
		t.Fatal("druid should not expose --cwd")
	}
}
