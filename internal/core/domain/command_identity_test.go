package domain

import (
	"strings"
	"testing"
)

func TestCommandIdentityValidation(t *testing.T) {
	for _, permission := range []string{"commands:list", "queue:read", "commands:run", "commands:create", "commands:remove", "logs:read", "console:write", "runtime:read", "ui:publish"} {
		if !ValidCommandPermission(permission) {
			t.Fatalf("known permission rejected: %s", permission)
		}
	}
	if ValidCommandPermission("owner:all") {
		t.Fatal("unknown permission accepted")
	}
	scroll, err := NewScrollFromBytes("", []byte("name: identity\ndesc: Identity fixture\nversion: 0.1.0\napp_version: 1.0\ncommands:\n  automation:\n    identity:\n      permissions: [owner:all]\n    procedures:\n      - image: busybox\n"))
	if err == nil {
		err = scroll.Validate(false)
	}
	if err == nil || !strings.Contains(err.Error(), "unknown identity permission") {
		t.Fatalf("unknown permission was not rejected: %v", err)
	}
}
