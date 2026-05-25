package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

func (b *Backend) StopRuntime(root string) error {
	if root == "" {
		return fmt.Errorf("runtime root is required")
	}
	ctx := context.Background()
	items, err := b.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "druid.root-hash="+rootHash(root))),
	})
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := b.client.ContainerRemove(ctx, item.ID, container.RemoveOptions{Force: true}); err != nil {
			return err
		}
	}
	b.mu.Lock()
	for key := range b.containers {
		delete(b.containers, key)
	}
	for key := range b.stdin {
		delete(b.stdin, key)
	}
	b.mu.Unlock()
	return nil
}

func (b *Backend) DeleteRuntime(root string, purgeData bool) error {
	if err := b.StopRuntime(root); err != nil {
		return err
	}
	if err := b.deleteUIPackages(context.Background(), runtimeID(root)); err != nil {
		return err
	}
	if purgeData {
		ref, err := ParseRootRef(root)
		if err != nil {
			return err
		}
		if ref.Kind == StorageVolume {
			return b.client.VolumeRemove(context.Background(), ref.Source, true)
		}
		return b.emptyRoot(context.Background(), root)
	}
	return nil
}
