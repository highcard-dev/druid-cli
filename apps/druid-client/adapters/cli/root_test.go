package cli

import "testing"

func TestRootCommandExposesOCICommands(t *testing.T) {
	root := NewRootCommand()
	for _, name := range []string{"pull", "push", "login", "register"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd == nil || cmd.Name() != name {
			t.Fatalf("druid-client should expose %q", name)
		}
	}
	cmd, _, err := root.Find([]string{"push", "category"})
	if err != nil || cmd == nil || cmd.Name() != "category" {
		t.Fatalf("druid-client should expose push category")
	}
}

func TestRegisterRejectsDirectoryWithoutScrollYAML(t *testing.T) {
	cmd := (&App{}).registerCmd()
	err := cmd.RunE(cmd, []string{t.TempDir()})
	if err == nil {
		t.Fatal("register should reject directory without scroll.yaml")
	}
}

func TestRootCommandIsSocketOnly(t *testing.T) {
	root := NewRootCommand()
	if flag := root.PersistentFlags().Lookup("daemon-url"); flag != nil {
		t.Fatal("druid-client should not expose --daemon-url")
	}
	if flag := root.PersistentFlags().Lookup("daemon-socket"); flag == nil {
		t.Fatal("druid-client should expose --daemon-socket")
	}
}

func TestRootCommandDoesNotExposeCWDFlag(t *testing.T) {
	root := NewRootCommand()
	if flag := root.PersistentFlags().Lookup("cwd"); flag != nil {
		t.Fatal("druid-client should not expose --cwd")
	}
}

func TestCreateAndRegisterDoNotExposeRuntimeFlag(t *testing.T) {
	app := &App{}
	if flag := app.createCmd().Flags().Lookup("runtime"); flag != nil {
		t.Fatal("druid-client create should not expose --runtime")
	}
	if flag := app.registerCmd().Flags().Lookup("runtime"); flag != nil {
		t.Fatal("druid-client register should not expose --runtime")
	}
}
