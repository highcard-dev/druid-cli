package workloadidentity

import "testing"

func TestCredentialRedaction(t *testing.T) {
	token := "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJmaXh0dXJlIn0.c2ln"
	if got := Redact("echo " + token); got != "echo [REDACTED]" {
		t.Fatal("credential exposed")
	}
}
