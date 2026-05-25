package docker

import (
	"context"
	"io"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/image"
)

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
