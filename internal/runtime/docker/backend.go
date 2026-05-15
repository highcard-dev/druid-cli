package docker

import (
	"archive/tar"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type Backend struct {
	client         *client.Client
	consoleManager ports.ConsoleManagerInterface
	config         Config
	mu             sync.Mutex
	containers     map[string]string
	stdin          map[string]io.Writer
}

type Config struct {
	WorkerImage  string
	Network      string
	Storage      string
	BindRoot     string
	VolumePrefix string
}

const dockerHostGatewayExtraHost = "host.docker.internal:host-gateway"

func (c Config) WithDefaults() Config {
	if c.WorkerImage == "" {
		c.WorkerImage = os.Getenv("DRUID_DOCKER_WORKER_IMAGE")
	}
	if c.Network == "" {
		c.Network = os.Getenv("DRUID_DOCKER_NETWORK")
	}
	if c.Storage == "" {
		c.Storage = os.Getenv("DRUID_DOCKER_STORAGE")
	}
	if c.Storage == "" {
		c.Storage = StorageVolume
	}
	if c.BindRoot == "" {
		c.BindRoot = os.Getenv("DRUID_DOCKER_BIND_ROOT")
	}
	if c.VolumePrefix == "" {
		c.VolumePrefix = os.Getenv("DRUID_DOCKER_VOLUME_PREFIX")
	}
	if c.VolumePrefix == "" {
		c.VolumePrefix = "druid"
	}
	return c
}

func New(consoleManager ports.ConsoleManagerInterface) (*Backend, error) {
	return NewWithConfig(Config{}, consoleManager)
}

func NewWithConfig(config Config, consoleManager ports.ConsoleManagerInterface) (*Backend, error) {
	config = config.WithDefaults()
	if config.Storage != StorageVolume && config.Storage != StorageBind {
		return nil, fmt.Errorf("unknown docker storage %q", config.Storage)
	}
	if config.Storage == StorageBind {
		if config.BindRoot == "" {
			return nil, fmt.Errorf("docker bind root is required when docker storage is bind")
		}
		if !filepath.IsAbs(config.BindRoot) {
			return nil, fmt.Errorf("docker bind root must be absolute: %s", config.BindRoot)
		}
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Backend{
		client:         cli,
		consoleManager: consoleManager,
		config:         config,
		containers:     map[string]string{},
		stdin:          map[string]io.Writer{},
	}, nil
}

func (b *Backend) Name() string {
	return "docker"
}

func (b *Backend) RootRef(id string, _ string) string {
	root, err := b.config.RuntimeRootRef(id)
	if err != nil {
		return id
	}
	return root
}

func (b *Backend) ReadScrollFile(root string) ([]byte, error) {
	if root == "" {
		return nil, fmt.Errorf("runtime root is required")
	}
	return b.readRootFile(context.Background(), root, "scroll.yaml")
}

func (b *Backend) RunCommand(command ports.RuntimeCommand) (*int, error) {
	for idx, procedure := range command.Command.Procedures {
		procedureName := domain.ProcedureName(command.Name, idx, procedure)
		env := command.ProcedureEnv[procedureName]
		if env == nil {
			env = procedure.Env
		}
		if command.Command.Run == domain.RunModePersistent {
			if procedure.IsSignal() {
				if err := b.Signal(procedureName, procedure.Target, procedure.Signal, command.Root); err != nil {
					return nil, err
				}
				continue
			}
			if procedure.Image == "" {
				return nil, fmt.Errorf("docker runtime procedure %s requires image", procedureName)
			}
			if err := b.startPersistentContainer(runtimeConsoleID(command.ScrollID, procedureName), procedureName, procedureResourceName(command.Name, idx), procedure, command.Root, command.GlobalPorts, env); err != nil {
				return nil, err
			}
			continue
		}
		exitCode, err := b.runProcedure(runtimeConsoleID(command.ScrollID, procedureName), procedureName, procedureResourceName(command.Name, idx), procedure, command.Root, command.GlobalPorts, env)
		if err != nil {
			return exitCode, err
		}
		if exitCode != nil && *exitCode != 0 {
			if procedure.IgnoreFailure {
				continue
			}
			return exitCode, nil
		}
	}
	return nil, nil
}

func (b *Backend) runProcedure(consoleID string, procedureName string, resourceName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, env map[string]string) (*int, error) {
	if procedure.IsSignal() {
		return nil, b.Signal(procedureName, procedure.Target, procedure.Signal, root)
	}
	if procedure.Image == "" {
		return nil, fmt.Errorf("docker runtime procedure %s requires image", procedureName)
	}
	return b.runContainer(consoleID, procedureName, resourceName, procedure, root, globalPorts, env)
}

func (b *Backend) ExpectedPorts(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimePortStatus, error) {
	statuses := []domain.RuntimePortStatus{}
	portsByName := portsByName(globalPorts)
	for commandName, command := range commands {
		if command == nil {
			continue
		}
		for idx, procedure := range command.Procedures {
			if procedure == nil || len(procedure.ExpectedPorts) == 0 {
				continue
			}
			procedureName := fmt.Sprintf("%s.%d", commandName, idx)
			if procedure.Id != nil {
				procedureName = *procedure.Id
			}
			containerStatuses, err := b.expectedPortsForProcedure(root, procedureName, procedure, portsByName)
			if err != nil {
				return nil, err
			}
			statuses = append(statuses, containerStatuses...)
		}
	}
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].Procedure == statuses[j].Procedure {
			return statuses[i].Name < statuses[j].Name
		}
		return statuses[i].Procedure < statuses[j].Procedure
	})
	return statuses, nil
}

func (b *Backend) RoutingTargets(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimeRoutingTarget, error) {
	portsByName := portsByName(globalPorts)
	targets := []domain.RuntimeRoutingTarget{{
		Name:        "webdav",
		Procedure:   "dev",
		PortName:    "webdav",
		Port:        8084,
		Protocol:    "https",
		ServiceName: ContainerName(root, "dev"),
		ServicePort: 8084,
	}}
	seen := map[string]struct{}{"webdav": {}}
	commandNames := make([]string, 0, len(commands))
	for commandName := range commands {
		commandNames = append(commandNames, commandName)
	}
	sort.Strings(commandNames)
	for _, commandName := range commandNames {
		command := commands[commandName]
		if command == nil {
			continue
		}
		for idx, procedure := range command.Procedures {
			if procedure == nil || len(procedure.ExpectedPorts) == 0 {
				continue
			}
			procedureName := domain.ProcedureName(commandName, idx, procedure)
			serviceName := ContainerName(root, procedureResourceName(commandName, idx))
			for _, expectedPort := range procedure.ExpectedPorts {
				if _, ok := seen[expectedPort.Name]; ok {
					continue
				}
				port, ok := portsByName[expectedPort.Name]
				if !ok {
					return nil, fmt.Errorf("expected port %s is not defined in top-level ports", expectedPort.Name)
				}
				seen[expectedPort.Name] = struct{}{}
				targets = append(targets, domain.RuntimeRoutingTarget{
					Name:        expectedPort.Name,
					Procedure:   procedureName,
					PortName:    expectedPort.Name,
					Port:        port.Port,
					Protocol:    normalizeProtocol(port.Protocol),
					ServiceName: serviceName,
					ServicePort: port.Port,
				})
			}
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	return targets, nil
}

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
	hostConfig := &container.HostConfig{Mounts: []mount.Mount{rootMount}, ExtraHosts: []string{dockerHostGatewayExtraHost}}
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

func (b *Backend) Attach(commandName string, data string) error {
	b.mu.Lock()
	stdin := b.stdin[commandName]
	b.mu.Unlock()
	if stdin == nil {
		return fmt.Errorf("target container %s not attached", commandName)
	}
	_, err := stdin.Write([]byte(data))
	return err
}

func (b *Backend) Signal(_ string, target string, signal string, root string) error {
	if target == "" {
		return nil
	}
	ctx := context.Background()
	containerID := b.containerID(target, root)
	options := container.StopOptions{}
	if signal != "" {
		options.Signal = signal
	}
	return b.client.ContainerStop(ctx, containerID, options)
}

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

func (b *Backend) RestoreRuntime(ctx context.Context, root string, artifact string, registryCredentials []domain.RegistryCredential) error {
	if artifact == "" {
		return fmt.Errorf("restore artifact is required")
	}
	if err := b.StopRuntime(root); err != nil {
		return err
	}
	return b.runWorkerRootCommand(ctx, root, []string{
		"worker", "pull",
		"--artifact", artifact,
		"--runtime-id", rootHash(root),
		"--mode", string(ports.RuntimeWorkerModeCreate),
		"--root", "/scroll",
	}, registryCredentials)
}

func (b *Backend) readRootFile(ctx context.Context, root string, relativePath string) ([]byte, error) {
	var data []byte
	err := b.withHelperContainer(ctx, root, func(containerID string) error {
		reader, _, err := b.client.CopyFromContainer(ctx, containerID, "/scroll/"+relativePath)
		if err != nil {
			return err
		}
		defer reader.Close()
		tarReader := tar.NewReader(reader)
		for {
			header, err := tarReader.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return fmt.Errorf("file %s not found in root", relativePath)
				}
				return err
			}
			if header.Typeflag == tar.TypeReg {
				data, err = io.ReadAll(tarReader)
				return err
			}
		}
	})
	return data, err
}

func (b *Backend) emptyRoot(ctx context.Context, root string) error {
	return b.withHelperContainer(ctx, root, func(containerID string) error {
		return b.runContainerCommand(ctx, containerID, []string{"sh", "-c", "find /scroll -mindepth 1 -maxdepth 1 -exec rm -rf {} +"})
	})
}

func (b *Backend) prepareWritableRoot(ctx context.Context, root string) error {
	return b.withHelperContainer(ctx, root, func(containerID string) error {
		return b.runContainerCommand(ctx, containerID, []string{"sh", "-c", "mkdir -p /scroll/data /scroll/.druid && chmod -R a+rwX /scroll"})
	})
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
	hostConfig := &container.HostConfig{Mounts: []mount.Mount{rootMount}, ExtraHosts: []string{dockerHostGatewayExtraHost}}
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
	return b.withHelperContainer(ctx, root, func(containerID string) error {
		return b.runContainerCommand(ctx, containerID, append([]string{"mkdir", "-p"}, paths...))
	})
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

func (b *Backend) withHelperContainer(ctx context.Context, root string, fn func(containerID string) error) error {
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
	hostConfig := &container.HostConfig{Mounts: []mount.Mount{rootMount}, ExtraHosts: []string{dockerHostGatewayExtraHost}}
	if b.config.Network != "" {
		hostConfig.NetworkMode = container.NetworkMode(b.config.Network)
	}
	name := fmt.Sprintf("druid-helper-%s-%d", rootHash(root), time.Now().UnixNano())
	created, err := b.client.ContainerCreate(ctx, &container.Config{
		Image:      b.config.WorkerImage,
		User:       "0",
		Entrypoint: []string{"/bin/sh", "-c"},
		Cmd:        []string{"sleep 300"},
		Labels: map[string]string{
			"druid.helper":    "root",
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
	return fn(created.ID)
}

func (b *Backend) runContainerCommand(ctx context.Context, containerID string, command []string) error {
	execID, err := b.client.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          command,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return err
	}
	attach, err := b.client.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
	if err != nil {
		return err
	}
	var output strings.Builder
	_, _ = io.Copy(&output, attach.Reader)
	attach.Close()
	inspect, err := b.client.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return err
	}
	if inspect.ExitCode != 0 {
		return fmt.Errorf("helper command exited with %d: %s", inspect.ExitCode, strings.TrimSpace(output.String()))
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
	hostConfig := &container.HostConfig{Mounts: mounts, ExtraHosts: []string{dockerHostGatewayExtraHost}}
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

func (b *Backend) runContainer(consoleID string, commandName string, resourceName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, env map[string]string) (*int, error) {
	ctx := context.Background()
	if procedure.Image == "" {
		return nil, errors.New("docker image is required")
	}
	if err := b.ensureVolumeSubpathSupport(ctx, root, procedure.Mounts); err != nil {
		return nil, err
	}
	if err := b.ensureProcedureMountPaths(ctx, root, procedure.Mounts); err != nil {
		return nil, err
	}

	if err := b.pullImage(ctx, procedure.Image); err != nil {
		return nil, err
	}

	config, hostConfig, err := containerSpec(commandName, procedure, root, globalPorts, env)
	if err != nil {
		return nil, err
	}
	containerName := ContainerName(root, resourceName)
	_ = b.client.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})

	created, err := b.client.ContainerCreate(ctx, config, hostConfig, nil, nil, containerName)
	if err != nil {
		return nil, err
	}
	b.setContainer(commandName, created.ID)
	defer func() {
		b.clearContainer(commandName)
		_ = b.client.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
	}()

	attach, err := b.client.ContainerAttach(ctx, created.ID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return nil, err
	}
	defer attach.Close()
	b.setStdin(commandName, attach.Conn)
	defer b.clearStdin(commandName)

	combined := make(chan string, 20)
	consoleType := domain.ConsoleTypeContainer
	if procedure.TTY {
		consoleType = domain.ConsoleTypeTTY
	}
	console, doneChan := b.consoleManager.AddConsoleWithChannel(consoleID, consoleType, "stdin", combined)
	console.WriteInput = func(data string) error {
		return b.Attach(commandName, data)
	}

	var copyWG sync.WaitGroup
	copyWG.Add(1)
	go func() {
		defer copyWG.Done()
		defer close(combined)
		writer := channelWriter{channel: combined}
		if procedure.TTY {
			_, _ = io.Copy(writer, attach.Reader)
			return
		}
		_, _ = stdcopy.StdCopy(writer, writer, attach.Reader)
	}()

	if err := b.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return nil, err
	}

	statusCh, errCh := b.client.ContainerWait(ctx, created.ID, container.WaitConditionNotRunning)
	var exitCode int
	select {
	case waitErr := <-errCh:
		if waitErr != nil {
			return nil, waitErr
		}
	case status := <-statusCh:
		exitCode = int(status.StatusCode)
	}
	_ = attach.CloseWrite()
	copyWG.Wait()
	console.MarkExited(exitCode)
	<-doneChan
	return &exitCode, nil
}

func (b *Backend) startPersistentContainer(consoleID string, commandName string, resourceName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, env map[string]string) error {
	ctx := context.Background()
	if procedure.Image == "" {
		return errors.New("docker image is required")
	}
	if err := b.ensureVolumeSubpathSupport(ctx, root, procedure.Mounts); err != nil {
		return err
	}
	if err := b.ensureProcedureMountPaths(ctx, root, procedure.Mounts); err != nil {
		return err
	}
	if err := b.pullImage(ctx, procedure.Image); err != nil {
		return err
	}
	config, hostConfig, err := containerSpec(commandName, procedure, root, globalPorts, env)
	if err != nil {
		return err
	}
	containerName := ContainerName(root, resourceName)
	_ = b.client.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
	created, err := b.client.ContainerCreate(ctx, config, hostConfig, nil, nil, containerName)
	if err != nil {
		return err
	}
	attach, err := b.client.ContainerAttach(ctx, created.ID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		_ = b.client.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
		return err
	}
	b.setContainer(commandName, created.ID)
	b.setStdin(commandName, attach.Conn)

	combined := make(chan string, 20)
	consoleType := domain.ConsoleTypeContainer
	if procedure.TTY {
		consoleType = domain.ConsoleTypeTTY
	}
	console, _ := b.consoleManager.AddConsoleWithChannel(consoleID, consoleType, "stdin", combined)
	console.WriteInput = func(data string) error {
		return b.Attach(commandName, data)
	}

	go func() {
		defer close(combined)
		defer attach.Close()
		writer := channelWriter{channel: combined}
		if procedure.TTY {
			_, _ = io.Copy(writer, attach.Reader)
			return
		}
		_, _ = stdcopy.StdCopy(writer, writer, attach.Reader)
	}()

	if err := b.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		attach.Close()
		b.clearContainer(commandName)
		b.clearStdin(commandName)
		_ = b.client.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
		return err
	}

	go func() {
		statusCh, errCh := b.client.ContainerWait(context.Background(), created.ID, container.WaitConditionNotRunning)
		select {
		case <-errCh:
		case status := <-statusCh:
			exitCode := int(status.StatusCode)
			console.MarkExited(exitCode)
		}
		b.clearContainer(commandName)
		b.clearStdin(commandName)
	}()
	return nil
}

func (b *Backend) pullImage(ctx context.Context, imageRef string) error {
	if _, err := b.client.ImageInspect(ctx, imageRef); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return err
	}
	reader, err := b.client.ImagePull(ctx, imageRef, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)
	return nil
}

func (b *Backend) containerID(commandName string, root string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id := b.containers[commandName]; id != "" {
		return id
	}
	return ContainerName(root, commandName)
}

func (b *Backend) setContainer(commandName string, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.containers[commandName] = id
}

func (b *Backend) clearContainer(commandName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.containers, commandName)
}

func (b *Backend) setStdin(commandName string, stdin io.Writer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stdin[commandName] = stdin
}

func (b *Backend) clearStdin(commandName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.stdin, commandName)
}

type channelWriter struct {
	channel chan<- string
}

func (w channelWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.channel <- string(p)
	}
	return len(p), nil
}

func containerSpec(commandName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, env map[string]string) (*container.Config, *container.HostConfig, error) {
	if procedure.Image == "" {
		return nil, nil, errors.New("docker image is required")
	}

	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}
	for _, expectedPort := range procedure.ExpectedPorts {
		port, ok := portsByName(globalPorts)[expectedPort.Name]
		if !ok {
			return nil, nil, fmt.Errorf("expected port %s is not defined in top-level ports", expectedPort.Name)
		}
		protocol := port.Protocol
		if protocol == "" || protocol == "http" || protocol == "https" {
			protocol = "tcp"
		}
		dockerPort := nat.Port(fmt.Sprintf("%d/%s", port.Port, protocol))
		exposedPorts[dockerPort] = struct{}{}
		portBindings[dockerPort] = []nat.PortBinding{{HostPort: fmt.Sprintf("%d", port.Port)}}
	}

	mounts := []mount.Mount{}
	for _, mount := range procedure.Mounts {
		if mount.Path == "" {
			return nil, nil, fmt.Errorf("mount path is required")
		}
		dockerMount, err := DockerMount(root, mount.Path, mount.ReadOnly, procedureDataSubPath(mount.SubPath))
		if err != nil {
			return nil, nil, err
		}
		mounts = append(mounts, dockerMount)
	}

	return &container.Config{
			Image:        procedure.Image,
			Cmd:          procedure.Command,
			WorkingDir:   procedure.WorkingDir,
			Env:          envArgs(env),
			ExposedPorts: exposedPorts,
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			OpenStdin:    true,
			Tty:          procedure.TTY,
			Labels: map[string]string{
				"druid.command":   commandName,
				"druid.root-hash": rootHash(root),
			},
		}, &container.HostConfig{
			Mounts:       mounts,
			PortBindings: portBindings,
			ExtraHosts:   []string{dockerHostGatewayExtraHost},
		}, nil
}

func procedureDataSubPath(subPath string) string {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(subPath, "/")))
	if subPath == "" {
		return domain.RuntimeDataDir
	}
	if clean == "." {
		return "."
	}
	if clean == domain.RuntimeDataDir || strings.HasPrefix(clean, domain.RuntimeDataDir+"/") {
		return clean
	}
	return filepath.ToSlash(filepath.Join(domain.RuntimeDataDir, filepath.FromSlash(clean)))
}

func ContainerName(root string, commandName string) string {
	return sanitizeContainerName(fmt.Sprintf("%s-%s", runtimeID(root), commandName))
}

func runtimeID(root string) string {
	switch {
	case strings.HasPrefix(root, "docker-volume://"):
		name := strings.TrimPrefix(root, "docker-volume://")
		if strings.HasPrefix(name, "druid-") && strings.HasSuffix(name, "-data") {
			return strings.TrimSuffix(strings.TrimPrefix(name, "druid-"), "-data")
		}
		return name
	case strings.HasPrefix(root, "docker-bind://"):
		return strings.TrimSuffix(filepath.Base(strings.TrimPrefix(root, "docker-bind://")), "-data")
	default:
		return strings.TrimSuffix(filepath.Base(root), "-data")
	}
}

func procedureResourceName(commandName string, procedureIndex int) string {
	return fmt.Sprintf("%s-%d", commandName, procedureIndex)
}

func rootHash(root string) string {
	hash := sha1.Sum([]byte(root))
	return hex.EncodeToString(hash[:])[:10]
}

func runtimeConsoleID(scrollID string, procedureName string) string {
	if scrollID == "" {
		return procedureName
	}
	return scrollID + "/" + procedureName
}

func sanitizeContainerName(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)
	name = re.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-_.")
	if name == "" {
		return "command"
	}
	return name
}

func envArgs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys))
	for _, key := range keys {
		args = append(args, fmt.Sprintf("%s=%s", key, env[key]))
	}
	return args
}

func dockerWorkerEnv(base []string) []string {
	if plainHTTP := os.Getenv("DRUID_REGISTRY_PLAIN_HTTP"); plainHTTP != "" {
		base = append(base, "DRUID_REGISTRY_PLAIN_HTTP="+plainHTTP)
	}
	return base
}

type ContainerSpec struct {
	Image        string
	Command      []string
	WorkingDir   string
	Env          []string
	Mounts       []mount.Mount
	PortBindings nat.PortMap
	TTY          bool
}

func BuildContainerSpec(commandName string, procedure *domain.Procedure, root string, globalPorts []domain.Port) (*ContainerSpec, error) {
	return BuildContainerSpecWithEnv(commandName, procedure, root, globalPorts, procedure.Env)
}

func BuildContainerSpecWithEnv(commandName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, env map[string]string) (*ContainerSpec, error) {
	config, hostConfig, err := containerSpec(commandName, procedure, root, globalPorts, env)
	if err != nil {
		return nil, err
	}
	return &ContainerSpec{
		Image:        config.Image,
		Command:      config.Cmd,
		WorkingDir:   config.WorkingDir,
		Env:          config.Env,
		Mounts:       hostConfig.Mounts,
		PortBindings: hostConfig.PortBindings,
		TTY:          config.Tty,
	}, nil
}

type containerTraffic struct {
	containerID    string
	rxBytes        uint64
	txBytes        uint64
	lastDeltaRX    uint64
	lastActivityAt *time.Time
	samples        []trafficSample
}

type trafficSample struct {
	at time.Time
	rx uint64
	tx uint64
}

type trafficStore struct {
	mu             sync.Mutex
	samples        map[string][]trafficSample
	lastActivityAt map[string]time.Time
}

var globalTrafficStore = &trafficStore{
	samples:        map[string][]trafficSample{},
	lastActivityAt: map[string]time.Time{},
}

func (s *trafficStore) record(containerID string, rxBytes uint64, txBytes uint64, now time.Time) containerTraffic {
	s.mu.Lock()
	defer s.mu.Unlock()
	samples := s.samples[containerID]
	var lastDeltaRX uint64
	if len(samples) > 0 && rxBytes >= samples[len(samples)-1].rx {
		lastDeltaRX = rxBytes - samples[len(samples)-1].rx
		if lastDeltaRX > 0 {
			s.lastActivityAt[containerID] = now
		}
	}
	samples = append(samples, trafficSample{at: now, rx: rxBytes, tx: txBytes})
	cutoff := now.Add(-24 * time.Hour)
	keepFrom := 0
	for keepFrom < len(samples) && samples[keepFrom].at.Before(cutoff) {
		keepFrom++
	}
	samples = samples[keepFrom:]
	s.samples[containerID] = samples

	var lastActivityAt *time.Time
	if last, ok := s.lastActivityAt[containerID]; ok {
		lastCopy := last
		lastActivityAt = &lastCopy
	}
	samplesCopy := append([]trafficSample(nil), samples...)
	return containerTraffic{
		containerID:    containerID,
		rxBytes:        rxBytes,
		txBytes:        txBytes,
		lastDeltaRX:    lastDeltaRX,
		lastActivityAt: lastActivityAt,
		samples:        samplesCopy,
	}
}

func (t containerTraffic) rxDelta(window time.Duration, now time.Time) uint64 {
	if window <= 0 || len(t.samples) == 0 {
		return t.lastDeltaRX
	}
	cutoff := now.Add(-window)
	base := t.samples[0]
	for _, sample := range t.samples {
		if !sample.at.Before(cutoff) {
			base = sample
			break
		}
	}
	if t.rxBytes < base.rx {
		return 0
	}
	return t.rxBytes - base.rx
}

func (b *Backend) expectedPortsForProcedure(root string, procedureName string, procedure *domain.Procedure, ports map[string]domain.Port) ([]domain.RuntimePortStatus, error) {
	statuses := make([]domain.RuntimePortStatus, 0, len(procedure.ExpectedPorts))
	containerName := ContainerName(root, procedureName)
	ctx := context.Background()
	inspected, err := b.client.ContainerInspect(ctx, containerName)
	containerFound := err == nil
	if err != nil && !cerrdefs.IsNotFound(err) {
		return nil, err
	}

	var traffic *containerTraffic
	if containerFound {
		if sample, err := b.containerTraffic(ctx, inspected.ID); err == nil {
			traffic = sample
		}
	}

	for _, expectedPort := range procedure.ExpectedPorts {
		port, ok := ports[expectedPort.Name]
		if !ok {
			return nil, fmt.Errorf("expected port %s is not defined in top-level ports", expectedPort.Name)
		}
		status := domain.RuntimePortStatus{
			Name:             expectedPort.Name,
			Procedure:        procedureName,
			Port:             port.Port,
			Protocol:         normalizeProtocol(port.Protocol),
			KeepAliveTraffic: expectedPort.KeepAliveTraffic,
			Source:           "docker-container-stats",
		}
		if containerFound {
			status.Bound, status.HostIP, status.HostPort = dockerPortBinding(inspected.NetworkSettings.Ports, port)
		}
		if traffic != nil {
			rx := traffic.rxBytes
			tx := traffic.txBytes
			status.RXBytes = &rx
			status.TXBytes = &tx
			status.LastActivityAt = traffic.lastActivityAt
			delta := traffic.lastDeltaRX
			if expectedPort.KeepAliveTraffic != "" {
				threshold, err := domain.ParseKeepAliveTraffic(expectedPort.KeepAliveTraffic)
				if err != nil {
					return nil, err
				}
				delta = traffic.rxDelta(threshold.Window, time.Now())
				trafficOK := delta >= threshold.Bytes
				status.TrafficOK = &trafficOK
				status.TrafficWindow = threshold.Window.String()
			}
			status.Traffic = delta > 0
			status.TrafficBytes = &delta
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (b *Backend) containerTraffic(ctx context.Context, containerID string) (*containerTraffic, error) {
	stats, err := b.client.ContainerStats(ctx, containerID, false)
	if err != nil {
		return nil, err
	}
	defer stats.Body.Close()
	var response container.StatsResponse
	if err := json.NewDecoder(stats.Body).Decode(&response); err != nil {
		return nil, err
	}
	var rxBytes uint64
	var txBytes uint64
	for _, network := range response.Networks {
		rxBytes += network.RxBytes
		txBytes += network.TxBytes
	}
	traffic := globalTrafficStore.record(containerID, rxBytes, txBytes, time.Now())
	return &traffic, nil
}

func dockerPortBinding(bindings nat.PortMap, port domain.Port) (bool, string, int) {
	dockerPort := nat.Port(fmt.Sprintf("%d/%s", port.Port, normalizeProtocol(port.Protocol)))
	portBindings := bindings[dockerPort]
	if len(portBindings) == 0 {
		return false, "", 0
	}
	hostPort, _ := strconv.Atoi(portBindings[0].HostPort)
	return true, portBindings[0].HostIP, hostPort
}

func portsByName(ports []domain.Port) map[string]domain.Port {
	result := make(map[string]domain.Port, len(ports))
	for _, port := range ports {
		result[port.Name] = port
	}
	return result
}

func normalizeProtocol(protocol string) string {
	protocol = strings.ToLower(protocol)
	if protocol == "" || protocol == "http" || protocol == "https" {
		return "tcp"
	}
	return protocol
}
