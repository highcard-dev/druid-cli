package docker

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/highcard-dev/daemon/internal/core/domain"
)

func (b *Backend) emptyRoot(ctx context.Context, root string) error {
	_, err := b.runRootHelper(ctx, root, []string{"sh", "-c", "chmod -R a+rwX /scroll 2>/dev/null || true; find /scroll -mindepth 1 -maxdepth 1 -exec rm -rf {} +; chmod a+rwX /scroll"})
	return err
}

func (b *Backend) prepareWritableRoot(ctx context.Context, root string) error {
	_, err := b.runRootHelper(ctx, root, []string{"sh", "-c", "mkdir -p /scroll/data /scroll/.druid && chmod -R a+rwX /scroll"})
	return err
}

func (b *Backend) ensureProcedureMountPaths(ctx context.Context, root string, mounts []domain.Mount) error {
	if len(mounts) == 0 {
		return nil
	}
	ref, err := ParseRootRef(root)
	if err != nil {
		return err
	}
	if ref.Kind == StorageBind {
		return nil
	}
	paths := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		cleaned, err := cleanRootSubPath(procedureDataSubPath(mount.SubPath))
		if err != nil {
			return err
		}
		paths = append(paths, "/scroll/"+cleaned)
	}
	_, err = b.runRootHelper(ctx, root, append([]string{"mkdir", "-p"}, paths...))
	return err
}

func (b *Backend) ensureVolumeSubpathSupport(ctx context.Context, root string, mounts []domain.Mount) error {
	if len(mounts) == 0 {
		return nil
	}
	ref, err := ParseRootRef(root)
	if err != nil {
		return err
	}
	if ref.Kind != StorageVolume {
		return nil
	}
	version, err := b.client.ServerVersion(ctx)
	if err != nil {
		return err
	}
	if !dockerAPIVersionAtLeast(version.APIVersion, 1, 45) {
		return fmt.Errorf("docker volume subpath mounts require Docker API >= 1.45, got %s", version.APIVersion)
	}
	return nil
}

func dockerAPIVersionAtLeast(version string, wantMajor int, wantMinor int) bool {
	majorText, minorText, ok := strings.Cut(version, ".")
	if !ok {
		return false
	}
	major, err := strconv.Atoi(majorText)
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(minorText)
	if err != nil {
		return false
	}
	if major != wantMajor {
		return major > wantMajor
	}
	return minor >= wantMinor
}

func (b *Backend) runRootHelper(ctx context.Context, root string, command []string) ([]byte, error) {
	if b.config.WorkerImage == "" {
		return nil, fmt.Errorf("docker worker image is required; set --docker-worker-image or DRUID_DOCKER_WORKER_IMAGE")
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("docker root helper command is required")
	}
	rootMount, err := DockerMount(root, "/scroll", false, "")
	if err != nil {
		return nil, err
	}
	if err := b.pullImage(ctx, b.config.WorkerImage); err != nil {
		return nil, err
	}
	hostConfig := &container.HostConfig{Mounts: []mount.Mount{rootMount}, ExtraHosts: dockerExtraHosts()}
	if b.config.Network != "" {
		hostConfig.NetworkMode = container.NetworkMode(b.config.Network)
	}
	name := fmt.Sprintf("druid-helper-%s-%d", rootHash(root), time.Now().UnixNano())
	created, err := b.client.ContainerCreate(ctx, &container.Config{
		Image:      b.config.WorkerImage,
		User:       "0",
		Entrypoint: []string{command[0]},
		Cmd:        command[1:],
		Labels: map[string]string{
			"druid.helper":    "root",
			"druid.root-hash": rootHash(root),
		},
	}, hostConfig, nil, nil, name)
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
		return output.Bytes(), fmt.Errorf("helper container exited with %d: %s", statusCode, strings.TrimSpace(output.String()))
	}
	return output.Bytes(), nil
}
