package ports

import (
	"context"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
)

// CommandIdentityBackend is implemented by both concrete command runtimes.
// It does not replace infrastructure ServiceAccount authentication.
type CommandIdentityBackend interface {
	WorkloadIdentities() workloadidentity.Registry
	ActiveExecutions(context.Context) (map[string]bool, error)
}

type CommandConsoleBackend interface {
	ReadConsoleLogs(context.Context, string, string, int, int64) (string, bool, error)
	WriteConsoleInput(context.Context, string, string, []byte) (int, error)
}
