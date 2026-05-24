package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (b *Backend) RunCommand(command ports.RuntimeCommand) (*int, error) {
	for idx, procedure := range command.Command.Procedures {
		procedureName := domain.ProcedureName(command.Name, idx, procedure)
		env := command.ProcedureEnv[procedureName]
		if env == nil {
			env = procedure.Env
		}
		logger.Log().Info("Starting Docker procedure",
			zap.String("scroll_id", command.ScrollID),
			zap.String("command", command.Name),
			zap.String("procedure", procedureName),
			zap.String("resource", procedureResourceName(command.Name, idx)),
			zap.String("run_mode", string(command.Command.Run)),
			zap.String("image", procedure.Image),
			zap.Bool("signal", procedure.IsSignal()),
			zap.Int("expected_ports", len(procedure.ExpectedPorts)),
		)
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
			if err := b.startPersistentContainer(runtimeConsoleID(command.ScrollID, procedureName), command.Name, procedureName, procedureResourceName(command.Name, idx), procedure, command.Root, command.GlobalPorts, command.Routing, env); err != nil {
				return nil, err
			}
			continue
		}
		exitCode, err := b.runProcedure(runtimeConsoleID(command.ScrollID, procedureName), command.Name, procedureName, procedureResourceName(command.Name, idx), procedure, command.Root, command.GlobalPorts, command.Routing, env)
		if err != nil {
			if exitCode != nil && *exitCode != 0 && procedure.IgnoreFailure {
				continue
			}
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

func (b *Backend) runProcedure(consoleID string, commandName string, procedureName string, resourceName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, routing []domain.RuntimeRouteAssignment, env map[string]string) (*int, error) {
	if procedure.IsSignal() {
		return nil, b.Signal(procedureName, procedure.Target, procedure.Signal, root)
	}
	if procedure.Image == "" {
		return nil, fmt.Errorf("docker runtime procedure %s requires image", procedureName)
	}
	return b.runContainer(consoleID, commandName, procedureName, resourceName, procedure, root, globalPorts, routing, env)
}

func (b *Backend) runContainer(consoleID string, commandName string, procedureName string, resourceName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, routing []domain.RuntimeRouteAssignment, env map[string]string) (*int, error) {
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

	config, hostConfig, err := containerSpec(commandName, procedure, root, globalPorts, routing, env)
	if err != nil {
		return nil, err
	}
	config.Labels = dockerProcedureLabels(root, commandName, procedureName, resourceName, 0, config.Labels)
	selected, err := b.createOrReuseProcedureContainer(ctx, root, commandName, procedureName, resourceName, config, hostConfig)
	if err != nil {
		return nil, dockerSetupError(err)
	}
	b.setContainer(procedureName, selected.ID)
	defer func() {
		b.clearContainer(procedureName)
	}()

	attach, err := b.client.ContainerAttach(ctx, selected.ID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return nil, err
	}
	defer attach.Close()
	b.setStdin(procedureName, attach.Conn)
	defer b.clearStdin(procedureName)

	combined := make(chan string, 20)
	consoleType := domain.ConsoleTypeContainer
	if procedure.TTY {
		consoleType = domain.ConsoleTypeTTY
	}
	console, doneChan := b.consoleManager.AddConsoleWithChannel(consoleID, consoleType, "stdin", combined)
	console.WriteInput = func(data string) error {
		return b.Attach(procedureName, data)
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

	if selected.Start {
		if err := b.client.ContainerStart(ctx, selected.ID, container.StartOptions{}); err != nil {
			_ = b.client.ContainerRemove(context.Background(), selected.ID, container.RemoveOptions{Force: true})
			return nil, dockerSetupError(err)
		}
	}

	statusCh, errCh := b.client.ContainerWait(ctx, selected.ID, container.WaitConditionNotRunning)
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
	if exitCode == 0 {
		_ = b.client.ContainerRemove(context.Background(), selected.ID, container.RemoveOptions{Force: true})
		return &exitCode, nil
	}
	return &exitCode, &domain.CommandExecutionError{
		Command:  commandName,
		ExitCode: exitCode,
		Err:      b.containerFailureError(context.Background(), selected.ID, selected.Name, procedure.Image, exitCode),
	}
}

func (b *Backend) startPersistentContainer(consoleID string, commandName string, procedureName string, resourceName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, routing []domain.RuntimeRouteAssignment, env map[string]string) error {
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
	config, hostConfig, err := containerSpec(commandName, procedure, root, globalPorts, routing, env)
	if err != nil {
		return err
	}
	config.Labels = dockerProcedureLabels(root, commandName, procedureName, resourceName, 0, config.Labels)
	selected, err := b.createOrReuseProcedureContainer(ctx, root, commandName, procedureName, resourceName, config, hostConfig)
	if err != nil {
		return dockerSetupError(err)
	}
	attach, err := b.client.ContainerAttach(ctx, selected.ID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		if selected.Start {
			_ = b.client.ContainerRemove(context.Background(), selected.ID, container.RemoveOptions{Force: true})
		}
		return err
	}
	b.setContainer(procedureName, selected.ID)
	b.setStdin(procedureName, attach.Conn)

	combined := make(chan string, 20)
	consoleType := domain.ConsoleTypeContainer
	if procedure.TTY {
		consoleType = domain.ConsoleTypeTTY
	}
	console, _ := b.consoleManager.AddConsoleWithChannel(consoleID, consoleType, "stdin", combined)
	console.WriteInput = func(data string) error {
		return b.Attach(procedureName, data)
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

	if selected.Start {
		if err := b.client.ContainerStart(ctx, selected.ID, container.StartOptions{}); err != nil {
			attach.Close()
			b.clearContainer(procedureName)
			b.clearStdin(procedureName)
			_ = b.client.ContainerRemove(context.Background(), selected.ID, container.RemoveOptions{Force: true})
			return dockerSetupError(err)
		}
	}

	go func() {
		statusCh, errCh := b.client.ContainerWait(context.Background(), selected.ID, container.WaitConditionNotRunning)
		select {
		case <-errCh:
		case status := <-statusCh:
			exitCode := int(status.StatusCode)
			console.MarkExited(exitCode)
			if exitCode == 0 {
				_ = b.client.ContainerRemove(context.Background(), selected.ID, container.RemoveOptions{Force: true})
			} else {
				logger.Log().Error("Docker persistent procedure exited",
					zap.String("container", selected.Name),
					zap.String("container_id", selected.ID),
					zap.String("image", procedure.Image),
					zap.Int("exit_code", exitCode),
					zap.Error(b.containerFailureError(context.Background(), selected.ID, selected.Name, procedure.Image, exitCode)),
				)
			}
		}
		b.clearContainer(procedureName)
		b.clearStdin(procedureName)
	}()
	return nil
}
