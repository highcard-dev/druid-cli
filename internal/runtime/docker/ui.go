package docker

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

const uiPackagePort = "8085/tcp"

func (b *Backend) PublishUIPackage(ctx context.Context, action ports.RuntimeUIPackageAction) (ports.RuntimeUIPackageResult, error) {
	if b.config.WorkerImage == "" {
		return ports.RuntimeUIPackageResult{}, fmt.Errorf("docker ui publishing requires --docker-worker-image or DRUID_DOCKER_WORKER_IMAGE")
	}
	if action.RuntimeID == "" || action.RootRef == "" || action.SourcePath == "" {
		return ports.RuntimeUIPackageResult{}, fmt.Errorf("ui package publish requires runtime id, root, and source path")
	}
	if err := b.pullImage(ctx, b.config.WorkerImage); err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	if err := b.ensureUIPackageServer(ctx); err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	hash, err := b.copyUIPackage(ctx, action)
	if err != nil {
		return ports.RuntimeUIPackageResult{}, err
	}
	base := strings.TrimRight(b.config.WithDefaults().UIPublicURL, "/")
	return ports.RuntimeUIPackageResult{
		URL:    fmt.Sprintf("%s/%s/%s/%s/app.wasm", base, sanitizeContainerName(action.RuntimeID), action.Scope, hash),
		Path:   action.SourcePath,
		SHA256: hash,
	}, nil
}

func (b *Backend) ensureUIPackageServer(ctx context.Context) error {
	name := b.uiPackageServerName()
	existing, err := b.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "^/"+name+"$")),
	})
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		if existing[0].State == "running" {
			return nil
		}
		_ = b.client.ContainerRemove(ctx, existing[0].ID, container.RemoveOptions{Force: true})
	}
	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{b.uiPackagesMount("/packages", true)},
		PortBindings: nat.PortMap{
			uiPackagePort: []nat.PortBinding{{HostIP: hostFromBind(b.config.UIBind), HostPort: portFromBind(b.config.UIBind)}},
		},
		ExtraHosts: dockerExtraHosts(),
	}
	if b.config.Network != "" {
		hostConfig.NetworkMode = container.NetworkMode(b.config.Network)
	}
	created, err := b.client.ContainerCreate(ctx, &container.Config{
		Image:        b.config.WorkerImage,
		Entrypoint:   []string{"druid"},
		Cmd:          []string{"ui", "serve", "--root", "/packages", "--listen", ":8085"},
		ExposedPorts: nat.PortSet{uiPackagePort: struct{}{}},
		Labels: map[string]string{
			"druid.role": "ui-packages",
		},
	}, hostConfig, nil, nil, name)
	if err != nil {
		return dockerSetupError(err)
	}
	if err := b.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		_ = b.client.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
		return dockerSetupError(err)
	}
	return nil
}

func (b *Backend) copyUIPackage(ctx context.Context, action ports.RuntimeUIPackageAction) (string, error) {
	rootMount, err := DockerMount(action.RootRef, "/scroll", true, "")
	if err != nil {
		return "", err
	}
	command := fmt.Sprintf(
		`set -eu; src="/scroll/%s"; test -f "$src" || { echo "UI package not found at %s; run the build command first" >&2; exit 66; }; sha="$(sha256sum "$src" | awk '{print $1}')"; dst="/packages/%s/%s/$sha"; mkdir -p "$dst"; cp "$src" "$dst/app.wasm"; printf '%%s' "$sha"`,
		action.SourcePath,
		action.SourcePath,
		sanitizeContainerName(action.RuntimeID),
		action.Scope,
	)
	output, err := b.runUIPackageCommand(ctx, []mount.Mount{rootMount, b.uiPackagesMount("/packages", false)}, []string{"sh", "-c", command})
	if err != nil {
		return "", err
	}
	hash := strings.TrimSpace(string(output))
	if hash == "" {
		return "", fmt.Errorf("ui package helper did not return a content hash")
	}
	return hash, nil
}

func (b *Backend) deleteUIPackages(ctx context.Context, runtimeID string) error {
	if runtimeID == "" || b.config.WorkerImage == "" {
		return nil
	}
	if err := b.pullImage(ctx, b.config.WorkerImage); err != nil {
		return err
	}
	_, err := b.runUIPackageCommand(ctx, []mount.Mount{b.uiPackagesMount("/packages", false)}, []string{"rm", "-rf", "/packages/" + sanitizeContainerName(runtimeID)})
	return err
}

func (b *Backend) runUIPackageCommand(ctx context.Context, mounts []mount.Mount, command []string) ([]byte, error) {
	name := fmt.Sprintf("druid-ui-helper-%s", rootHash(strings.Join(command, " ")))
	created, err := b.client.ContainerCreate(ctx, &container.Config{
		Image:      b.config.WorkerImage,
		User:       "0",
		Entrypoint: []string{command[0]},
		Cmd:        command[1:],
		Labels: map[string]string{
			"druid.role": "ui-helper",
		},
	}, &container.HostConfig{Mounts: mounts, ExtraHosts: dockerExtraHosts()}, nil, nil, name)
	if cerrdefs.IsConflict(err) {
		_ = b.client.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})
		created, err = b.client.ContainerCreate(ctx, &container.Config{
			Image:      b.config.WorkerImage,
			User:       "0",
			Entrypoint: []string{command[0]},
			Cmd:        command[1:],
			Labels:     map[string]string{"druid.role": "ui-helper"},
		}, &container.HostConfig{Mounts: mounts, ExtraHosts: dockerExtraHosts()}, nil, nil, name)
	}
	if err != nil {
		return nil, err
	}
	defer b.client.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
	if err := b.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return nil, err
	}
	statusCh, errCh := b.client.ContainerWait(ctx, created.ID, container.WaitConditionNotRunning)
	var statusCode int64
	select {
	case err := <-errCh:
		if err != nil {
			return nil, err
		}
	case status := <-statusCh:
		statusCode = status.StatusCode
	}
	logs, err := b.client.ContainerLogs(context.Background(), created.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return nil, err
	}
	defer logs.Close()
	var output bytes.Buffer
	if _, err := stdcopy.StdCopy(&output, &output, logs); err != nil {
		return nil, err
	}
	if statusCode != 0 {
		return output.Bytes(), fmt.Errorf("ui package helper exited with %d: %s", statusCode, strings.TrimSpace(output.String()))
	}
	return output.Bytes(), nil
}

func (b *Backend) uiPackagesMount(target string, readOnly bool) mount.Mount {
	return mount.Mount{Type: mount.TypeVolume, Source: sanitizeVolumePart(b.config.VolumePrefix + "-ui-packages"), Target: target, ReadOnly: readOnly}
}

func (b *Backend) uiPackageServerName() string {
	return sanitizeContainerName(b.config.VolumePrefix + "-ui-packages")
}

func hostFromBind(bind string) string {
	host, _, ok := strings.Cut(bind, ":")
	if !ok || host == "" {
		return "127.0.0.1"
	}
	return host
}

func portFromBind(bind string) string {
	_, port, ok := strings.Cut(bind, ":")
	if !ok || port == "" {
		return "8085"
	}
	return port
}
