package ports

import (
	"context"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
)

// CommandIdentityBackend is implemented by runtimes that authenticate command workloads.
// It does not replace infrastructure ServiceAccount authentication.
type CommandIdentityBackend interface {
	PrepareCommandIdentity(context.Context, string) error
	AuthenticateCommandIdentity(context.Context, string) (workloadidentity.Identity, error)
}

type CommandConsoleBackend interface {
	ReadConsoleLogs(context.Context, string, string, int, int64) (string, bool, error)
	WriteConsoleInput(context.Context, string, string, []byte) (int, error)
}
