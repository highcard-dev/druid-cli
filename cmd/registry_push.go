package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/highcard-dev/daemon/internal/core/domain"
	logger "github.com/highcard-dev/daemon/internal/core/services/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

var PushCommand = &cobra.Command{
	Use:   "push",
	Short: "Generate OCI Artifacts and push to a remote registry",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		ctx := context.Background()

		user := viper.GetString("registry.user")
		password := viper.GetString("registry.password")
		host := viper.GetString("registry.host")

		if user == "" || password == "" || host == "" {
			return fmt.Errorf("registry host, user and password must be set. Please use `druid registry login` to set them")
		}

		folder := "."
		if len(args) == 1 {
			folder = args[0]
		}

		scroll, err := domain.NewScroll(folder)

		if err != nil {
			return err
		}

		repo := scroll.Name

		tag := scroll.AppVersion

		// 0. Create a file store
		fs, err := file.New(folder)
		if err != nil {
			return err
		}
		defer fs.Close()

		mediaType := "druid/scroll"
		fileNames := []string{"init-files", "init-files-template", "scroll-switch", "update", "scroll.yaml"}

		filesToPush := []string{}

		for _, d := range fileNames {
			if _, err := os.Stat(filepath.Join(folder, d)); !os.IsNotExist(err) {
				filesToPush = append(filesToPush, d)
			}
		}

		fileDescriptors := make([]v1.Descriptor, 0, len(filesToPush))
		for _, name := range filesToPush {
			fileDescriptor, err := fs.Add(ctx, name, mediaType, "")
			if err != nil {
				return err
			}
			fileDescriptors = append(fileDescriptors, fileDescriptor)
			logger.Log().Info(fmt.Sprintf("file descriptor for %s: %v\n", name, fileDescriptor.Digest))
		}

		artifactType := "druid/scroll"
		manifestDescriptor, err := oras.Pack(ctx, fs, artifactType, fileDescriptors, oras.PackOptions{
			PackImageManifest:   true,
			ManifestAnnotations: map[string]string{},
		})
		if err != nil {
			return err
		}

		repoInstance, err := remote.NewRepository(repo)
		if err != nil {
			return err
		}
		// Note: The below code can be omitted if authentication is not required
		repoInstance.Client = &auth.Client{
			Client: retry.DefaultClient,
			Cache:  auth.DefaultCache,
			Credential: auth.StaticCredential(host, auth.Credential{
				Username: user,
				Password: password,
			}),
		}

		if err = fs.Tag(ctx, manifestDescriptor, tag.String()); err != nil {
			return err
		}
		_, err = oras.Copy(ctx, fs, tag.String(), repoInstance, tag.String(), oras.DefaultCopyOptions)
		if err != nil {
			return err
		}

		logger.Log().Info("Pushed to registry")
		return nil
	},
}
