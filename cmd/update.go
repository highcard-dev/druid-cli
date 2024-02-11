package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/logger"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var UpdateCommand = &cobra.Command{
	Use:   "update",
	Short: "Check for current version of the Scroll and update if necessary",
	Long:  ``,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		scrollDir := utils.GetScrollDirFromCwd(cwd)
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

		repo, tag := utils.SplitArtifact(artifact)

		//ctx := context.Background()
		logger.Log().Info("Starting update Scroll Files command")

		host := viper.GetString("registry.host")
		user := viper.GetString("registry.user")
		password := viper.GetString("registry.password")

		registryClient := registry.NewOciClient(host, user, password)

		fileName := utils.GetScrollDirFromCwd(cwd) + "/manifest.json"
		b, err := os.ReadFile(fileName)

		if err != nil {
			return fmt.Errorf("error reading manifest file: %v", err)
		}

		var manifest v1.Descriptor
		err = json.Unmarshal(b, &manifest)
		if err != nil {
			return fmt.Errorf("error unmarshalling manifest file: %v", err)
		}

		logger.Log().Info("Checking for updates for " + artifact)
		canUpdate, err := registryClient.CanUpdateTag(manifest, repo, tag)

		if err != nil {
			return fmt.Errorf("error checking for updates: %v", err)
		}
		if canUpdate {
			logger.Log().Info("Updated scroll files")
			registryClient.Pull(scrollDir, artifact)
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
}
