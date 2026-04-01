package services_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/services"
)

const minimalScrollYaml = `name: test-scroll
version: 0.0.1
commands: {}
`

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func TestRenderCwdTemplates_BootstrapsScrollConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "scroll.yaml"), minimalScrollYaml)
	writeFile(t, filepath.Join(dir, "scroll-config.yml.scroll_template"), "key: generated-value\n")

	svc, err := services.NewScrollService(dir)
	if err != nil {
		t.Fatalf("NewScrollService: %v", err)
	}

	if err := svc.RenderCwdTemplates(); err != nil {
		t.Fatalf("RenderCwdTemplates: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "scroll-config.yml"))
	if err != nil {
		t.Fatalf("scroll-config.yml should exist after bootstrap: %v", err)
	}
	if string(content) != "key: generated-value\n" {
		t.Errorf("unexpected config content: %q", string(content))
	}
}

func TestRenderCwdTemplates_DoesNotOverwriteExistingConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "scroll.yaml"), minimalScrollYaml)
	writeFile(t, filepath.Join(dir, "scroll-config.yml"), "key: user-edited\n")
	writeFile(t, filepath.Join(dir, "scroll-config.yml.scroll_template"), "key: {{ randAlphaNum 50 }}\n")

	svc, err := services.NewScrollService(dir)
	if err != nil {
		t.Fatalf("NewScrollService: %v", err)
	}

	if err := svc.RenderCwdTemplates(); err != nil {
		t.Fatalf("RenderCwdTemplates: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "scroll-config.yml"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if string(content) != "key: user-edited\n" {
		t.Errorf("existing config was overwritten: %q", string(content))
	}
}

func TestRenderCwdTemplates_OtherTemplatesReceiveBootstrappedConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "scroll.yaml"), minimalScrollYaml)
	writeFile(t, filepath.Join(dir, "scroll-config.yml.scroll_template"), "greeting: hello\n")
	writeFile(t, filepath.Join(dir, "app.conf.scroll_template"), "value: {{ .Config.greeting }}\n")

	svc, err := services.NewScrollService(dir)
	if err != nil {
		t.Fatalf("NewScrollService: %v", err)
	}

	if err := svc.RenderCwdTemplates(); err != nil {
		t.Fatalf("RenderCwdTemplates: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "app.conf"))
	if err != nil {
		t.Fatalf("app.conf should exist: %v", err)
	}
	if string(content) != "value: hello\n" {
		t.Errorf("other template did not see bootstrapped config: %q", string(content))
	}
}
