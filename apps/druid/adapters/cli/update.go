package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var updateIncludeData bool

var UpdateCommand = &cobra.Command{
	Use:   "update [artifact] [dir]",
	Short: "Check for current version of the Scroll and update if necessary",
	Long:  ``,
	Args:  cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		scrollDir := currentWorkingDir()
		if len(args) == 2 {
			scrollDir = args[1]
		}

		var artifact string
		if len(args) > 0 {
			artifact = args[0]
		} else {
			scroll, err := domain.NewScroll(scrollDir)

			if err != nil {
				return err
			}
			artifact = scroll.Name + ":" + scroll.AppVersion
		}

		repo, ref, kind := utils.ParseArtifactRef(artifact)
		if repo == "" || ref == "" {
			return fmt.Errorf("invalid artifact reference %q (expected repo:tag or repo@sha256:digest)", artifact)
		}
		if kind == utils.ArtifactRefKindDigest {
			return fmt.Errorf("update only supports tag references (repo:tag). For digests, use `druid-client pull %s`", artifact)
		}
		tag := ref

		//ctx := context.Background()
		logger.Log().Info("Checking for updates for " + artifact)

		registryClient := registry.NewOciClient(loadUpdateRegistryStore())

		canUpdate := false

		fileName := filepath.Join(scrollDir, "manifest.json")
		b, err := os.ReadFile(fileName)

		if err != nil {
			logger.Log().Error("error reading manifest file, updating...")
			canUpdate = true
		} else {
			var manifest v1.Descriptor
			err = json.Unmarshal(b, &manifest)
			if err != nil {
				return fmt.Errorf("error unmarshalling manifest file: %v", err)
			}

			logger.Log().Info("Checking for updates for " + artifact)
			canUpdate, err = registryClient.CanUpdateTag(manifest, repo, tag)
			if err != nil {
				return fmt.Errorf("error checking for updates: %v", err)
			}
		}

		if canUpdate {
			logger.Log().Info("Updated scroll files")
			err = registryClient.PullSelective(scrollDir, artifact, updateIncludeData, nil)
			if err != nil {
				return fmt.Errorf("error pulling scroll files: %v", err)
			}
			logger.Log().Info("Scroll files updated")
		} else {
			logger.Log().Info("Scroll files are up to date")
		}

		return nil
	},
}

func init() {
	UpdateCommand.Flags().BoolVar(&updateIncludeData, "include-data", false, "Also pull scroll data layers")
}

func loadUpdateRegistryStore() *registry.CredentialStore {
	var registries []domain.RegistryCredential
	viper.UnmarshalKey("registries", &registries)
	if len(registries) == 0 {
		host := viper.GetString("registry.host")
		user := viper.GetString("registry.user")
		password := viper.GetString("registry.password")
		if host != "" {
			registries = append(registries, domain.RegistryCredential{
				Host:     host,
				Username: user,
				Password: password,
			})
		}
	}
	return registry.NewCredentialStore(registries)
}
