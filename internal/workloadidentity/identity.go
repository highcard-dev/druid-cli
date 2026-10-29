// Package workloadidentity implements command-execution credentials, distinct
// from owner JWTs and infrastructure Kubernetes ServiceAccount identities.
package workloadidentity

import (
	"errors"
	"regexp"
)

const TokenFile = "/run/secrets/druid/token"
const ExecutionLabel = "druid.execution-id"

var ErrUnauthorized = errors.New("invalid workload credential")
var ErrUnavailable = errors.New("workload identity verification is unavailable")
var tokenPattern = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)

type Record struct {
	RuntimeRoot  string   `json:"-"` // Backend runtime binding; not a daemon-session dependency.
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

func Redact(value string) string { return tokenPattern.ReplaceAllString(value, "[REDACTED]") }
