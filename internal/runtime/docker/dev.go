package docker

import (
	"context"
	"fmt"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

func (b *Backend) StartDev(ctx context.Context, action ports.RuntimeDevAction) error {
	if b.config.WorkerImage == "" {
		return fmt.Errorf("docker dev requires --docker-worker-image or DRUID_DOCKER_WORKER_IMAGE")
	}
	if action.RootRef == "" {
		return fmt.Errorf("dev root ref is required")
	}
	if action.MountPath == "" {
		action.MountPath = "/scroll"
	}
	if action.Listen == "" {
		action.Listen = ":8084"
	}
	if err := b.pullImage(ctx, b.config.WorkerImage); err != nil {
		return err
	}
	rootMount, err := DockerMount(action.RootRef, action.MountPath, false, "")
	if err != nil {
		return err
	}
	args := []string{
		"dev",
		"--root", action.MountPath,
		"--listen", action.Listen,
		"--runtime-id", action.RuntimeID,
		"--daemon-url", action.DaemonURL,
	}
	if action.DaemonToken != "" {
		args = append(args, "--daemon-token", action.DaemonToken)
	}
	if action.OwnerID != "" {
		args = append(args, "--owner-id", action.OwnerID)
	}
	if action.AuthJWKSURL != "" {
		args = append(args, "--auth-jwks-url", action.AuthJWKSURL)
	}
	if action.RuntimeJWKSURL != "" {
		args = append(args, "--runtime-jwks-url", action.RuntimeJWKSURL)
	}
	for _, path := range action.WatchPaths {
		args = append(args, "--watch", path)
	}
	for _, command := range action.HotReloadCommands {
		args = append(args, "--command", command)
	}
	hostConfig := &container.HostConfig{Mounts: []mount.Mount{rootMount}, ExtraHosts: dockerExtraHosts()}
	for _, assignment := range action.Routing {
		if assignment.PublicPort == 0 || (assignment.PortName != "webdav" && assignment.Name != "webdav") {
			continue
		}
		hostConfig.PortBindings = nat.PortMap{
			"8084/tcp": []nat.PortBinding{{
				HostIP:   assignment.ExternalIP,
				HostPort: fmt.Sprintf("%d", assignment.PublicPort),
			}},
		}
		break
	}
	if b.config.Network != "" {
		hostConfig.NetworkMode = container.NetworkMode(b.config.Network)
	}
	name := ContainerName(action.RootRef, "dev")
	_ = b.client.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})
	created, err := b.client.ContainerCreate(ctx, &container.Config{
		Image:        b.config.WorkerImage,
		Entrypoint:   []string{"druid"},
		Cmd:          args,
		ExposedPorts: nat.PortSet{"8084/tcp": struct{}{}},
		Labels: map[string]string{
			"druid.command":    "dev",
			"druid.runtime-id": action.RuntimeID,
			"druid.root-hash":  rootHash(action.RootRef),
		},
	}, hostConfig, nil, nil, name)
	if err != nil {
		return err
	}
	if err := b.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		_ = b.client.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
		return err
	}
	return nil
}

func (b *Backend) StopDev(ctx context.Context, root string) error {
	if root == "" {
		return fmt.Errorf("runtime root is required")
	}
	err := b.client.ContainerRemove(ctx, ContainerName(root, "dev"), container.RemoveOptions{Force: true})
	if err != nil && !cerrdefs.IsNotFound(err) {
		return err
	}
	return nil
}
