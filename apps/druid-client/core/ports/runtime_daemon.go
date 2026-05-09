package ports

import (
	"context"

	"github.com/highcard-dev/daemon/internal/api"
)

type RuntimeDaemon interface {
	CreateScroll(ctx context.Context, name string, artifact string, scrollRoot string, dataRoot string) (*api.RuntimeScroll, error)
	ListScrolls(ctx context.Context) ([]api.RuntimeScroll, error)
	GetScroll(ctx context.Context, id string) (*api.RuntimeScroll, error)
	DeleteScroll(ctx context.Context, id string) (*api.DeletedScroll, error)
	RunScrollCommand(ctx context.Context, id string, command string) (*api.RuntimeScroll, error)
	GetScrollPorts(ctx context.Context, id string) ([]api.RuntimePortStatus, error)
}

type ConsoleAttacher interface {
	Attach(ctx context.Context, scroll string, console string) error
}
