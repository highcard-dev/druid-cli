package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"

	logger "github.com/highcard-dev/daemon/internal/core/services/log"
	"github.com/highcard-dev/daemon/internal/utils"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.uber.org/zap"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

type OciClient struct {
	repo     string
	username string
	password string
}

func NewOciClient(repo string, username string, password string) *OciClient {
	return &OciClient{
		repo:     repo,
		username: username,
		password: password,
	}
}

func (c *OciClient) Pull(dir string, artifact string) error {

	repo, tag := utils.SplitArtifact(artifact)

	if tag == "" {
		return fmt.Errorf("tag must be set")
	}

	// 1. Connect to a remote repository
	ctx := context.Background()
	repoInstance, err := remote.NewRepository(repo)
	if err != nil {
		return err
	}
	if c.repo == "" {
		return fmt.Errorf("registry host must be set. Please use `druid registry login` to set them")
	}

	if c.username == "" || c.password == "" {
		logger.Log().Warn("No registry credentials found. Trying to pull anonymously")
	} else {
		repoInstance.Client = &auth.Client{
			Client: retry.DefaultClient,
			Cache:  auth.DefaultCache,
			Credential: auth.StaticCredential(c.repo, auth.Credential{
				Username: c.username,
				Password: c.password,
			}),
		}
	}

	fs, err := file.New(filepath.Join(dir))

	if err != nil {
		return err
	}

	// 2. Copy from the remote repository to the OCI layout store
	manifestDescriptor, err := oras.Copy(ctx, repoInstance, tag, fs, tag, oras.DefaultCopyOptions)
	if err != nil {
		return err
	}
	logger.Log().Info("Manifest pulled", zap.String("digest", manifestDescriptor.Digest.String()), zap.String("mediaType", manifestDescriptor.MediaType))

	jsonData, err := json.Marshal(&manifestDescriptor)

	if err != nil {
		return fmt.Errorf("failed to marshal manifest descriptor: %w", err)
	}

	fileName := filepath.Join(dir, "manifest.json")
	err = ioutil.WriteFile(fileName, jsonData, 0644)

	if err != nil {
		return fmt.Errorf("failed to write manifest descriptor: %w", err)
	}

	return nil

}

func (c *OciClient) CanUpdateTag(current v1.Descriptor, r string, tag string) (bool, error) {

	repo, err := remote.NewRepository(r)

	if err != nil {
		return false, err
	}
	disc, err := oras.Resolve(context.TODO(), repo, tag, oras.DefaultResolveOptions)
	if err != nil {
		return false, err
	}

	if current.Digest.String() != disc.Digest.String() {
		return true, nil
	}

	return false, nil

}
