package services

import (
	"context"

	"github.com/highcard-dev/daemon/apps/druid-client/core/ports"
	"github.com/highcard-dev/daemon/internal/api"
)

type RuntimeService struct {
	daemon ports.RuntimeDaemon
}

func NewRuntimeService(daemon ports.RuntimeDaemon) *RuntimeService {
	return &RuntimeService{daemon: daemon}
}

func (s *RuntimeService) Create(ctx context.Context, name string, artifact string, scrollRoot string, dataRoot string, start bool) (*api.RuntimeScroll, error) {
	return s.daemon.CreateScroll(ctx, name, artifact, scrollRoot, dataRoot, start)
}

func (s *RuntimeService) List(ctx context.Context) ([]api.RuntimeScroll, error) {
	return s.daemon.ListScrolls(ctx)
}

func (s *RuntimeService) Describe(ctx context.Context, id string) (*api.RuntimeScroll, error) {
	return s.daemon.GetScroll(ctx, id)
}

func (s *RuntimeService) Delete(ctx context.Context, id string) (*api.DeletedScroll, error) {
	return s.daemon.DeleteScroll(ctx, id)
}

func (s *RuntimeService) Run(ctx context.Context, id string, command string) (*api.RuntimeScroll, error) {
	return s.daemon.RunScrollCommand(ctx, id, command)
}

func (s *RuntimeService) Ports(ctx context.Context, id string) ([]api.RuntimePortStatus, error) {
	return s.daemon.GetScrollPorts(ctx, id)
}

func (s *RuntimeService) Start(ctx context.Context, id string) (*api.RuntimeScroll, error) {
	return s.daemon.StartScroll(ctx, id)
}

func (s *RuntimeService) Stop(ctx context.Context, id string) (*api.RuntimeScroll, error) {
	return s.daemon.StopScroll(ctx, id)
}

func (s *RuntimeService) RoutingTargets(ctx context.Context, id string) ([]api.RuntimeRoutingTarget, error) {
	return s.daemon.GetScrollRoutingTargets(ctx, id)
}

func (s *RuntimeService) ApplyRouting(ctx context.Context, id string, assignments []api.RuntimeRouteAssignment) (*api.RuntimeScroll, error) {
	return s.daemon.ApplyScrollRouting(ctx, id, assignments)
}
