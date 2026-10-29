package docker

import (
	"context"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
)

func (b *Backend) PrepareCommandIdentity(ctx context.Context, record workloadidentity.Record) (workloadidentity.CredentialMount, error) {
	if b.identities == nil {
		return workloadidentity.CredentialMount{}, workloadidentity.ErrUnavailable
	}
	return b.identities.Ensure(ctx, record)
}

func (b *Backend) AuthenticateCommandIdentity(ctx context.Context, token string) (workloadidentity.Record, error) {
	if b.identities == nil {
		return workloadidentity.Record{}, workloadidentity.ErrUnauthorized
	}
	return b.identities.Lookup(ctx, token)
}

func (b *Backend) ActiveExecutions(ctx context.Context) (map[string]bool, error) {
	items, err := b.client.ContainerList(ctx, container.ListOptions{All: true, Filters: filters.NewArgs(filters.Arg("label", workloadidentity.ExecutionLabel))})
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	for _, item := range items {
		if item.State == "running" || item.State == "restarting" || item.State == "created" || item.State == "paused" {
			active[item.Labels[workloadidentity.ExecutionLabel]] = true
		}
	}
	return active, nil
}
