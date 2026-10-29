package docker

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/pkg/stdcopy"
)

func (b *Backend) toolConsole(ctx context.Context, root, procedure string) (container.InspectResponse, error) {
	items, err := b.client.ContainerList(ctx, container.ListOptions{All: true, Filters: filters.NewArgs(
		filters.Arg("label", dockerLabelRole+"="+dockerRoleProcedure),
		filters.Arg("label", dockerLabelRootHash+"="+rootHash(root)),
		filters.Arg("label", dockerLabelProcedure+"="+procedure),
	)})
	if err != nil {
		return container.InspectResponse{}, err
	}
	if len(items) == 0 {
		return container.InspectResponse{}, fmt.Errorf("console is unavailable")
	}
	return b.client.ContainerInspect(ctx, selectConsoleContainer(items).ID)
}

func (b *Backend) ReadConsoleLogs(ctx context.Context, root, procedure string, tail int, maxBytes int64) (string, bool, error) {
	selected, err := b.toolConsole(ctx, root, procedure)
	if err != nil {
		return "", false, err
	}
	logs, err := b.client.ContainerLogs(ctx, selected.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true, Tail: strconv.Itoa(tail), Follow: false})
	if err != nil {
		return "", false, err
	}
	defer logs.Close()
	var output io.Reader = logs
	if selected.Config != nil && !selected.Config.Tty {
		reader, writer := io.Pipe()
		defer reader.Close()
		go func() { _, err := stdcopy.StdCopy(writer, writer, logs); _ = writer.CloseWithError(err) }()
		output = reader
	}
	data, err := io.ReadAll(io.LimitReader(output, maxBytes+1))
	if err != nil {
		return "", false, err
	}
	truncated := int64(len(data)) > maxBytes
	if truncated {
		data = data[:maxBytes]
	}
	return string(data), truncated, nil
}

func (b *Backend) WriteConsoleInput(ctx context.Context, root, procedure string, data []byte) (int, error) {
	selected, err := b.toolConsole(ctx, root, procedure)
	if err != nil {
		return 0, err
	}
	if selected.State == nil || !selected.State.Running {
		return 0, fmt.Errorf("console is not running")
	}
	attach, err := b.client.ContainerAttach(ctx, selected.ID, container.AttachOptions{Stream: true, Stdin: true})
	if err != nil {
		return 0, err
	}
	defer attach.Close()
	return attach.Conn.Write(data)
}
