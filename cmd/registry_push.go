package cmd

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var minRam string
var minCpu string
var minDisk string
var image string
var scrollPorts []string
var packMeta bool
var smart bool

var PushCommand = &cobra.Command{
	Use:   "push",
	Short: "Generate OCI Artifacts and push to a remote registry",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		credStore := LoadRegistryStore()
		if !credStore.HasCredentials() {
			return fmt.Errorf("no registry credentials configured. Please use `druid registry login` to set them")
		}

		folder := "."

		fullPath := path.Join(cwd, folder)

		scroll, err := domain.NewScroll(fullPath)

		if err != nil {
			return err
		}

		repo := scroll.Name
		tag := scroll.AppVersion

		if len(args) == 1 {
			repo, tag = utils.SplitArtifact(args[0])
		}

		logger.Log().Info("Pushing "+repo+":"+tag+" to registry", zap.String("path", fullPath))

		ociClient := registry.NewOciClient(credStore)

		overrides := map[string]string{}
		if minRam != "" {
			overrides["gg.druid.scroll.minRam"] = minRam
		}
		if minCpu != "" {
			overrides["gg.druid.scroll.minCpu"] = minCpu
		}
		if minDisk != "" {
			overrides["gg.druid.scroll.minDisk"] = minDisk
		}
		if image != "" {
			overrides["gg.druid.scroll.image"] = image
		}
		if smart {
			overrides["gg.druid.scroll.smart"] = "true"
		}
		for _, p := range scrollPorts {
			parts := strings.Split(p, "=")
			name := parts[0]
			port := "0"
			if len(parts) == 2 {
				port = parts[1]
			}
			overrides[fmt.Sprintf("gg.druid.scroll.port.%s", name)] = port
		}

		var tries int
		for tries < 3 {
			_, err = ociClient.Push(fullPath, repo, tag, overrides, packMeta, &scroll.File)
			if err != nil {
				tries++
				logger.Log().Error("Failed to push scroll to registry, retrying...", zap.Error(err), zap.Int("tries", tries))
				if tries >= 3 {
					logger.Log().Error("Failed to push scroll to registry after 3 attempts", zap.Error(err))
					return err
				}
			} else {
				break
			}
			time.Sleep(time.Duration(tries+1) * time.Second)
		}

		logger.Log().Info("Pushed "+scroll.Name+" to registry", zap.String("path", fullPath))
		return nil
	},
}

func init() {
	PushCommand.AddCommand(PushMetaCommand)

	PushCommand.Flags().StringVarP(&minRam, "min-ram", "r", minRam, "Minimum RAM required to run the application. (Will be added as a manifest annotation gg.druid.scroll.minRam)")
	PushCommand.Flags().StringVarP(&minCpu, "min-cpu", "c", minCpu, "Minimum CPU required to run the application. (Will be added as a manifest annotation gg.druid.scroll.minCpu)")
	PushCommand.Flags().StringVarP(&minDisk, "min-disk", "d", minDisk, "Minimum Disk required to run the application. (Will be added as a manifest annotation gg.druid.scroll.minDisk)")
	PushCommand.Flags().BoolVarP(&smart, "smart", "s", false, "Indicates, if the scroll is able to run as a smart deployment (Will be added as a manifest annotation gg.druid.scroll.smart)")

	PushCommand.Flags().StringVarP(&image, "image", "i", image, "Image to use for the scroll. (Will be added as a manifest annotation gg.druid.scroll.image)")

	PushCommand.Flags().StringSliceVarP(&scrollPorts, "port", "p", scrollPorts, "Ports to expose. Format webserver=80, dns=53/udp or just ftp (Will be added as a manifest annotation gg.druid.scroll.ports.<name>)")

	PushCommand.Flags().BoolVarP(&packMeta, "pack-meta", "m", packMeta, "Pack the meta folder into the scroll.")
}
