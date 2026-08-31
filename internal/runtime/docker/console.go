package docker

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/pkg/stdcopy"
)

type consoleSession struct {
	io.ReadCloser
	write func([]byte) (int, error)
	close func()
}

func (s *consoleSession) Write(data []byte) (int, error) {
	if s.write == nil {
		return 0, fmt.Errorf("console input is unavailable")
	}
	return s.write(data)
}

func (s *consoleSession) Close() error {
	if s.close != nil {
		s.close()
	}
	return s.ReadCloser.Close()
}

func (b *Backend) OpenConsole(ctx context.Context, root string, procedure string) (io.ReadWriteCloser, error) {
	selector := filters.NewArgs(
		filters.Arg("label", dockerLabelRole+"="+dockerRoleProcedure),
		filters.Arg("label", dockerLabelRootHash+"="+rootHash(root)),
		filters.Arg("label", dockerLabelProcedure+"="+procedure),
	)
	var items []container.Summary
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	for len(items) == 0 {
		var err error
		items, err = b.client.ContainerList(ctx, container.ListOptions{All: true, Filters: selector})
		if err != nil {
			return nil, err
		}
		if len(items) > 0 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("no container found for procedure %s", procedure)
		case <-poll.C:
		}
	}
	selected := selectConsoleContainer(items)
	inspected, err := b.client.ContainerInspect(ctx, selected.ID)
	if err != nil {
		return nil, err
	}
	running := inspected.State != nil && inspected.State.Running
	logs, err := b.client.ContainerLogs(ctx, selected.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true, Follow: running})
	if err != nil {
		return nil, err
	}
	output := io.ReadCloser(logs)
	if inspected.Config != nil && !inspected.Config.Tty {
		reader, writer := io.Pipe()
		go func() {
			_, copyErr := stdcopy.StdCopy(writer, writer, logs)
			_ = logs.Close()
			_ = writer.CloseWithError(copyErr)
		}()
		output = reader
	}
	session := &consoleSession{ReadCloser: output}
	if running {
		attach, attachErr := b.client.ContainerAttach(ctx, selected.ID, container.AttachOptions{Stream: true, Stdin: true})
		if attachErr != nil {
			_ = output.Close()
			return nil, attachErr
		}
		session.write = attach.Conn.Write
		session.close = func() { attach.Close() }
	}
	return session, nil
}

func selectConsoleContainer(items []container.Summary) container.Summary {
	selected := items[0]
	for _, candidate := range items[1:] {
		candidateRunning := candidate.State == "running"
		selectedRunning := selected.State == "running"
		if candidateRunning && !selectedRunning || candidateRunning == selectedRunning && candidate.Created > selected.Created {
			selected = candidate
		}
	}
	return selected
}

func (b *Backend) Signal(_ string, target string, signal string, root string) error {
	if target == "" {
		return nil
	}
	options := container.StopOptions{}
	if signal != "" {
		options.Signal = signal
	}
	return b.client.ContainerStop(context.Background(), b.containerID(target, root), options)
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
