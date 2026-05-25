package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
)

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
