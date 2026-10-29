package workloadidentity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileRegistryPreservesRevokesAndSeparatesCredentials(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	registry, err := NewFileRegistry(directory)
	if err != nil {
		t.Fatal(err)
	}
	record := Record{ExecutionID: "first", DeploymentID: "deployment", Command: "automation", Permissions: []string{"queue:read"}}
	mount, err := registry.Ensure(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(mount.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(token), TokenPrefix) || len(token) != len(TokenPrefix)+43 {
		t.Fatal("invalid token format")
	}
	data, err := os.ReadFile(filepath.Join(directory, "registry.json"))
	if err != nil || strings.Contains(string(data), string(token)) {
		t.Fatal("raw credential in authorization registry")
	}
	reopened, err := NewFileRegistry(directory)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := reopened.Lookup(ctx, string(token))
	if err != nil || identity.DeploymentID != record.DeploymentID || !identity.Allows("queue:read") || identity.Allows("commands:run") {
		t.Fatal("identity was not preserved")
	}
	remount, err := reopened.Ensure(ctx, record)
	if err != nil || remount.Path != mount.Path {
		t.Fatal("reattachment replaced the credential")
	}
	if err := reopened.Revoke(ctx, record.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Lookup(ctx, string(token)); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked token accepted")
	}
	if _, err := reopened.Ensure(ctx, record); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("late provisioning resurrected a revoked execution")
	}
	if _, err := os.Stat(mount.Path); !os.IsNotExist(err) {
		t.Fatal("revoked credential file retained")
	}
	if Redact("echo "+string(token)) != "echo [REDACTED]" {
		t.Fatal("credential was not redacted")
	}
	if _, err := reopened.Ensure(ctx, Record{ExecutionID: "../escape"}); err == nil {
		t.Fatal("unsafe credential path accepted")
	}
}
