package cli

import "testing"

func TestRootCommandExposesRuntimeAndOCICommands(t *testing.T) {
	for _, name := range []string{"pull", "push", "login", "dev"} {
		if cmd, _, err := RootCmd.Find([]string{name}); err != nil || cmd == nil || cmd.Name() != name {
			t.Fatalf("druid should expose %q", name)
		}
	}
	if cmd, _, err := RootCmd.Find([]string{"worker", "pull"}); err != nil || cmd == nil || cmd.Name() != "pull" {
		t.Fatalf("druid should expose worker pull")
	}
	if cmd, _, err := RootCmd.Find([]string{"worker", "push"}); err != nil || cmd == nil || cmd.Name() != "push" {
		t.Fatalf("druid should expose worker push")
	}
}

func TestDaemonCommandExposesRuntimeListeners(t *testing.T) {
	for _, name := range []string{"tcp", "port"} {
		if flag := DaemonCommand.Flags().Lookup(name); flag != nil {
			t.Fatalf("druid daemon should not expose --%s", name)
		}
	}
	for _, name := range []string{"socket", "listen", "public-listen", "internal-token", "worker-callback-listen", "worker-callback-url", "docker-storage", "docker-bind-root", "docker-volume-prefix"} {
		if flag := DaemonCommand.Flags().Lookup(name); flag == nil {
			t.Fatalf("druid daemon should expose --%s", name)
		}
	}
}

func TestRootCommandExposesDaemonTargets(t *testing.T) {
	for _, name := range []string{"daemon-url", "daemon-socket"} {
		if flag := RootCmd.PersistentFlags().Lookup(name); flag == nil {
			t.Fatalf("druid should expose --%s", name)
		}
	}
	if flag := RootCmd.PersistentFlags().Lookup("lo" + "cal"); flag != nil {
		t.Fatal("druid should not expose local direct execution")
	}
}

func TestRootCommandDoesNotExposeCWDFlag(t *testing.T) {
	if flag := RootCmd.PersistentFlags().Lookup("cwd"); flag != nil {
		t.Fatal("druid should not expose --cwd")
	}
}
