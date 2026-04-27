package services

import "testing"

func TestEnvWithDefaultTermAddsTermWhenMissing(t *testing.T) {
	env := envWithDefaultTerm([]string{"PATH=/usr/bin"})

	if got := env[len(env)-1]; got != "TERM=xterm-256color" {
		t.Fatalf("expected default TERM to be appended, got %q", got)
	}
}

func TestEnvWithDefaultTermReplacesEmptyTerm(t *testing.T) {
	env := envWithDefaultTerm([]string{"TERM=", "PATH=/usr/bin"})

	if got := env[0]; got != "TERM=xterm-256color" {
		t.Fatalf("expected empty TERM to be replaced, got %q", got)
	}
}

func TestEnvWithDefaultTermPreservesExistingTerm(t *testing.T) {
	env := envWithDefaultTerm([]string{"TERM=screen-256color", "PATH=/usr/bin"})

	if got := env[0]; got != "TERM=screen-256color" {
		t.Fatalf("expected existing TERM to be preserved, got %q", got)
	}
}
