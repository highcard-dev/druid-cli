// Package workloadidentity defines command-workload authorization, distinct
// from owner JWTs and infrastructure Kubernetes ServiceAccount identities.
package workloadidentity

import (
	"errors"
	"regexp"
)

const TokenFile = "/run/secrets/druid/token"

var ErrUnauthorized = errors.New("invalid workload credential")
var ErrUnavailable = errors.New("workload identity verification is unavailable")
var tokenPattern = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)

type Identity struct {
	DeploymentID string
	Permissions  []string
}

func (r Identity) Allows(permission string) bool {
	for _, allowed := range r.Permissions {
		if allowed == permission {
			return true
		}
	}
	return false
}

func Redact(value string) string { return tokenPattern.ReplaceAllString(value, "[REDACTED]") }
