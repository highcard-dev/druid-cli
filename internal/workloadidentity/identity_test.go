package workloadidentity

import (
	"strings"
	"testing"
)

func TestCredentialRedaction(t *testing.T) {
	for _, token := range []string{"druid_wl_" + strings.Repeat("A", 43), "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJmaXh0dXJlIn0.c2ln"} {
		if got := Redact("echo " + token); got != "echo [REDACTED]" {
			t.Fatal("credential exposed")
		}
	}
}
