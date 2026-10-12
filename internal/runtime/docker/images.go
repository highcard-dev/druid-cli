package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/image"
	"github.com/highcard-dev/daemon/internal/runtime/imagepolicy"
)

func (b *Backend) pullImage(ctx context.Context, imageRef string) error {
	if !imagepolicy.ShouldRefresh(imageRef) {
		if _, err := b.client.ImageInspect(ctx, imageRef); err == nil {
			return nil
		} else if !cerrdefs.IsNotFound(err) {
			return err
		}
	}
	reader, err := b.client.ImagePull(ctx, imageRef, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	for {
		var message struct {
			Error string `json:"error"`
		}
		if err := decoder.Decode(&message); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if message.Error != "" {
			return fmt.Errorf("pull image %s: %s", imageRef, message.Error)
		}
	}
}
