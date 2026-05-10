package docker

import (
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

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type Backend struct {
	client         *client.Client
	consoleManager ports.ConsoleManagerInterface
	mu             sync.Mutex
	containers     map[string]string
	stdin          map[string]io.Writer
}

func New(consoleManager ports.ConsoleManagerInterface) (*Backend, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Backend{
		client:         cli,
		consoleManager: consoleManager,
		containers:     map[string]string{},
		stdin:          map[string]io.Writer{},
	}, nil
}

func (b *Backend) Name() string {
	return "docker"
}

func (b *Backend) ReadScrollFile(scrollRoot string) ([]byte, error) {
	if scrollRoot == "" {
		return nil, fmt.Errorf("scroll root is required")
	}
	return os.ReadFile(filepath.Join(scrollRoot, "scroll.yaml"))
}

func (b *Backend) ReadDataFile(_ context.Context, dataRoot string, relativePath string) ([]byte, error) {
	filePath, err := dataFilePath(dataRoot, relativePath)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filePath)
}

func (b *Backend) WriteDataFile(_ context.Context, dataRoot string, relativePath string, data []byte) error {
	filePath, err := dataFilePath(dataRoot, relativePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

func dataFilePath(dataRoot string, relativePath string) (string, error) {
	cleaned := filepath.Clean(strings.TrimPrefix(relativePath, "/"))
	if cleaned == "." || cleaned == ".." || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid data file path %q", relativePath)
	}
	return filepath.Join(dataRoot, cleaned), nil
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
				if err := b.Signal(procedureName, procedure.Target, procedure.Signal, command.DataRoot); err != nil {
					return nil, err
				}
				continue
			}
			if procedure.Image == "" {
				return nil, fmt.Errorf("docker runtime procedure %s requires image", procedureName)
			}
			if err := b.startPersistentContainer(runtimeConsoleID(command.ScrollID, procedureName), procedureName, procedure, command.DataRoot, command.GlobalPorts, env); err != nil {
				return nil, err
			}
			continue
		}
		exitCode, err := b.runProcedure(runtimeConsoleID(command.ScrollID, procedureName), procedureName, procedure, command.DataRoot, command.GlobalPorts, env)
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

func (b *Backend) runProcedure(consoleID string, procedureName string, procedure *domain.Procedure, dataRoot string, globalPorts []domain.Port, env map[string]string) (*int, error) {
	if procedure.IsSignal() {
		return nil, b.Signal(procedureName, procedure.Target, procedure.Signal, dataRoot)
	}
	if procedure.Image == "" {
		return nil, fmt.Errorf("docker runtime procedure %s requires image", procedureName)
	}
	return b.runContainer(consoleID, procedureName, procedure, dataRoot, globalPorts, env)
}

func (b *Backend) ExpectedPorts(dataRoot string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimePortStatus, error) {
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
			containerStatuses, err := b.expectedPortsForProcedure(dataRoot, procedureName, procedure, portsByName)
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

func (b *Backend) Signal(_ string, target string, signal string, dataRoot string) error {
	if target == "" {
		return nil
	}
	ctx := context.Background()
	containerID := b.containerID(target, dataRoot)
	options := container.StopOptions{}
	if signal != "" {
		options.Signal = signal
	}
	return b.client.ContainerStop(ctx, containerID, options)
}

func (b *Backend) StopRuntime(dataRoot string) error {
	if dataRoot == "" {
		return fmt.Errorf("data root is required")
	}
	ctx := context.Background()
	items, err := b.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "druid.data-root-hash="+dataRootHash(dataRoot))),
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

func (b *Backend) DeleteRuntime(dataRoot string, purgeData bool) error {
	if err := b.StopRuntime(dataRoot); err != nil {
		return err
	}
	if purgeData {
		return os.RemoveAll(dataRoot)
	}
	return nil
}

func (b *Backend) runContainer(consoleID string, commandName string, procedure *domain.Procedure, dataRoot string, globalPorts []domain.Port, env map[string]string) (*int, error) {
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(dataRoot, domain.RuntimeDataDir), 0755); err != nil {
		return nil, err
	}
	if procedure.Image == "" {
		return nil, errors.New("docker image is required")
	}

	if err := b.pullImage(ctx, procedure.Image); err != nil {
		return nil, err
	}

	config, hostConfig, err := containerSpec(commandName, procedure, dataRoot, globalPorts, env)
	if err != nil {
		return nil, err
	}
	containerName := ContainerName(dataRoot, commandName)
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

func (b *Backend) startPersistentContainer(consoleID string, commandName string, procedure *domain.Procedure, dataRoot string, globalPorts []domain.Port, env map[string]string) error {
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(dataRoot, domain.RuntimeDataDir), 0755); err != nil {
		return err
	}
	if procedure.Image == "" {
		return errors.New("docker image is required")
	}
	if err := b.pullImage(ctx, procedure.Image); err != nil {
		return err
	}
	config, hostConfig, err := containerSpec(commandName, procedure, dataRoot, globalPorts, env)
	if err != nil {
		return err
	}
	containerName := ContainerName(dataRoot, commandName)
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
	reader, err := b.client.ImagePull(ctx, imageRef, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)
	return nil
}

func (b *Backend) containerID(commandName string, dataRoot string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id := b.containers[commandName]; id != "" {
		return id
	}
	return ContainerName(dataRoot, commandName)
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

func containerSpec(commandName string, procedure *domain.Procedure, dataRoot string, globalPorts []domain.Port, env map[string]string) (*container.Config, *container.HostConfig, error) {
	if procedure.Image == "" {
		return nil, nil, errors.New("docker image is required")
	}
	runtimeDataRoot := filepath.Join(dataRoot, domain.RuntimeDataDir)
	if err := os.MkdirAll(runtimeDataRoot, 0755); err != nil {
		return nil, nil, err
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

	binds := []string{}
	for _, mount := range procedure.Mounts {
		if mount.Path == "" {
			return nil, nil, fmt.Errorf("mount path is required")
		}
		subPath := mount.SubPath
		if subPath == "" {
			subPath = "."
		}
		hostPath := filepath.Join(runtimeDataRoot, filepath.FromSlash(subPath))
		if err := os.MkdirAll(hostPath, 0755); err != nil {
			return nil, nil, err
		}
		bind := fmt.Sprintf("%s:%s", hostPath, mount.Path)
		if mount.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
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
				"druid.command":        commandName,
				"druid.data-root-hash": dataRootHash(dataRoot),
			},
		}, &container.HostConfig{
			Binds:        binds,
			PortBindings: portBindings,
		}, nil
}

func ContainerName(scrollRoot string, commandName string) string {
	hash := sha1.Sum([]byte(scrollRoot))
	name := sanitizeContainerName(commandName)
	return fmt.Sprintf("druid-%s-%s", hex.EncodeToString(hash[:])[:10], name)
}

func dataRootHash(dataRoot string) string {
	hash := sha1.Sum([]byte(dataRoot))
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

type ContainerSpec struct {
	Image        string
	Command      []string
	WorkingDir   string
	Env          []string
	Binds        []string
	PortBindings nat.PortMap
	TTY          bool
}

func BuildContainerSpec(commandName string, procedure *domain.Procedure, dataRoot string, globalPorts []domain.Port) (*ContainerSpec, error) {
	return BuildContainerSpecWithEnv(commandName, procedure, dataRoot, globalPorts, procedure.Env)
}

func BuildContainerSpecWithEnv(commandName string, procedure *domain.Procedure, dataRoot string, globalPorts []domain.Port, env map[string]string) (*ContainerSpec, error) {
	config, hostConfig, err := containerSpec(commandName, procedure, dataRoot, globalPorts, env)
	if err != nil {
		return nil, err
	}
	return &ContainerSpec{
		Image:        config.Image,
		Command:      config.Cmd,
		WorkingDir:   config.WorkingDir,
		Env:          config.Env,
		Binds:        hostConfig.Binds,
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

func (b *Backend) expectedPortsForProcedure(dataRoot string, procedureName string, procedure *domain.Procedure, ports map[string]domain.Port) ([]domain.RuntimePortStatus, error) {
	statuses := make([]domain.RuntimePortStatus, 0, len(procedure.ExpectedPorts))
	containerName := ContainerName(dataRoot, procedureName)
	ctx := context.Background()
	inspected, err := b.client.ContainerInspect(ctx, containerName)
	containerFound := err == nil
	if err != nil && !client.IsErrNotFound(err) {
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
