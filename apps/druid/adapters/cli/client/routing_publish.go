package client

import (
	"context"

	"github.com/highcard-dev/daemon/internal/api"
	routingutil "github.com/highcard-dev/daemon/internal/routing"
)

func applyPublishedRouting(ctx context.Context, daemon RuntimeDaemon, id string, publishes []string) (*api.RuntimeScroll, error) {
	targets, err := daemon.GetScrollRoutingTargets(ctx, id)
	if err != nil {
		return nil, err
	}
	assignments, err := routingutil.AssignmentsFromPublishes(publishes, targets, id)
	if err != nil {
		return nil, err
	}
	return daemon.ApplyScrollRouting(ctx, id, assignments)
}
