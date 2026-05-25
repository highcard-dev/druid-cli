package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

func (b *Backend) BackupRuntime(ctx context.Context, root string, artifact string, registryCredentials []domain.RegistryCredential) error {
	if artifact == "" {
		return fmt.Errorf("backup artifact is required")
	}
	return b.runWorkerRootCommand(ctx, root, []string{
		"worker", "push",
		"--artifact", artifact,
		"--root", "/scroll",
	}, registryCredentials)
}

func (b *Backend) runWorkerRootCommand(ctx context.Context, root string, command []string, registryCredentials []domain.RegistryCredential) error {
	if b.config.WorkerImage == "" {
		return fmt.Errorf("docker worker image is required; set --docker-worker-image or DRUID_DOCKER_WORKER_IMAGE")
	}
	rootMount, err := DockerMount(root, "/scroll", false, "")
	if err != nil {
		return err
	}
	if err := b.pullImage(ctx, b.config.WorkerImage); err != nil {
		return err
	}
	if err := b.prepareWritableRoot(ctx, root); err != nil {
		return err
	}
	registryConfig, err := json.Marshal(struct {
		Registries []domain.RegistryCredential `json:"registries"`
	}{Registries: registryCredentials})
	if err != nil {
		return err
	}
	hostConfig := &container.HostConfig{Mounts: []mount.Mount{rootMount}, ExtraHosts: dockerExtraHosts()}
	if b.config.Network != "" {
		hostConfig.NetworkMode = container.NetworkMode(b.config.Network)
	}
	name := fmt.Sprintf("druid-worker-%s-%d", rootHash(root), time.Now().UnixNano())
	created, err := b.client.ContainerCreate(ctx, &container.Config{
		Image:      b.config.WorkerImage,
		Entrypoint: []string{"druid"},
		Cmd:        command,
		Env: dockerWorkerEnv([]string{
			"DRUID_RUNTIME_REGISTRY_CONFIG_JSON=" + string(registryConfig),
		}),
		Labels: map[string]string{
			"druid.worker":    "root",
			"druid.root-hash": rootHash(root),
		},
	}, hostConfig, nil, nil, name)
	if err != nil {
		return err
	}
	defer b.client.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
	if err := b.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return err
	}
	statusCh, errCh := b.client.ContainerWait(ctx, created.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			logs, _ := b.client.ContainerLogs(context.Background(), created.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true})
			defer func() {
				if logs != nil {
					logs.Close()
				}
			}()
			var message strings.Builder
			if logs != nil {
				_, _ = io.Copy(&message, logs)
			}
			return fmt.Errorf("worker container exited with %d: %s", status.StatusCode, strings.TrimSpace(message.String()))
		}
	}
	return nil
}

func (b *Backend) SpawnPullWorker(ctx context.Context, action ports.RuntimeWorkerAction) error {
	if b.config.WorkerImage == "" {
		return fmt.Errorf("docker worker image is required; set --docker-worker-image or DRUID_DOCKER_WORKER_IMAGE")
	}
	root := action.RootRef
	if root == "" {
		return fmt.Errorf("worker root ref is required")
	}
	if action.MountPath == "" {
		action.MountPath = "/scroll"
	}
	if err := b.pullImage(ctx, b.config.WorkerImage); err != nil {
		return err
	}
	if err := b.prepareWritableRoot(ctx, root); err != nil {
		return err
	}
	registryConfig, err := json.Marshal(struct {
		Registries []domain.RegistryCredential `json:"registries"`
	}{Registries: action.RegistryCredentials})
	if err != nil {
		return err
	}
	rootMount, err := DockerMount(root, action.MountPath, false, "")
	if err != nil {
		return err
	}
	artifact := action.Artifact
	mounts := []mount.Mount{rootMount}
	if info, statErr := os.Stat(action.Artifact); statErr == nil {
		abs, err := filepath.Abs(action.Artifact)
		if err != nil {
			return err
		}
		if info.IsDir() {
			mounts = append(mounts, mount.Mount{Type: mount.TypeBind, Source: abs, Target: "/artifact-src", ReadOnly: true})
			artifact = "/artifact-src"
		} else {
			mounts = append(mounts, mount.Mount{Type: mount.TypeBind, Source: filepath.Dir(abs), Target: "/artifact-src", ReadOnly: true})
			artifact = "/artifact-src/" + filepath.Base(abs)
		}
	}
	hostConfig := &container.HostConfig{Mounts: mounts, ExtraHosts: dockerExtraHosts()}
	if b.config.Network != "" {
		hostConfig.NetworkMode = container.NetworkMode(b.config.Network)
	}
	name := fmt.Sprintf("druid-worker-%s-%s", rootHash(root), rootHash(string(action.Mode)+action.Artifact))
	_ = b.client.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})
	created, err := b.client.ContainerCreate(ctx, &container.Config{
		Image:      b.config.WorkerImage,
		Entrypoint: []string{"druid"},
		Cmd: []string{
			"worker", "pull",
			"--artifact", artifact,
			"--runtime-id", action.RuntimeID,
			"--mode", string(action.Mode),
			"--root", action.MountPath,
			"--callback-url", action.CallbackURL,
		},
		Env: dockerWorkerEnv([]string{
			"DRUID_WORKER_TOKEN=" + action.CallbackToken,
			"DRUID_RUNTIME_REGISTRY_CONFIG_JSON=" + string(registryConfig),
		}),
		Labels: map[string]string{
			"druid.worker":     "pull",
			"druid.runtime-id": action.RuntimeID,
			"druid.root-hash":  rootHash(root),
		},
	}, hostConfig, nil, nil, name)
	if err != nil {
		return err
	}
	defer b.client.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
	if err := b.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return err
	}
	statusCh, errCh := b.client.ContainerWait(ctx, created.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			logs, _ := b.client.ContainerLogs(context.Background(), created.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true})
			defer func() {
				if logs != nil {
					logs.Close()
				}
			}()
			var message strings.Builder
			if logs != nil {
				_, _ = io.Copy(&message, logs)
			}
			return fmt.Errorf("worker container exited with %d: %s", status.StatusCode, strings.TrimSpace(message.String()))
		}
	}
	return nil
}
