// Package workloadidentity implements command-execution credentials, distinct
// from owner JWTs and infrastructure Kubernetes ServiceAccount identities.
package workloadidentity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
)

const TokenPrefix = "druid_wl_"
const TokenFile = "/run/secrets/druid/token"
const ExecutionLabel = "druid.execution-id"

var ErrUnauthorized = errors.New("invalid workload credential")
var ErrUnavailable = errors.New("workload identity verification is unavailable")
var tokenPattern = regexp.MustCompile(`druid_wl_[A-Za-z0-9_-]{43}|eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
var executionFileName = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

type Record struct {
	RuntimeRoot  string   `json:"-"` // Backend runtime binding; not a daemon-session dependency.
	Recover      bool     `json:"-"`
	ExecutionID  string   `json:"execution_id"`
	DeploymentID string   `json:"deployment_id"`
	Command      string   `json:"command"`
	Permissions  []string `json:"permissions"`
}

func (r Record) Allows(permission string) bool {
	for _, allowed := range r.Permissions {
		if allowed == permission {
			return true
		}
	}
	return false
}

// CredentialMount contains only a backend-owned reference, never raw token data.
type CredentialMount struct {
	Path string
}

type Registry interface {
	Ensure(context.Context, Record) (CredentialMount, error)
	Lookup(context.Context, string) (Record, error)
	Records(context.Context) ([]Record, error)
	Revoke(context.Context, string) error
	Prune(context.Context, map[string]bool) error
}

func NewToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func Hash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func Redact(value string) string { return tokenPattern.ReplaceAllString(value, "[REDACTED]") }
