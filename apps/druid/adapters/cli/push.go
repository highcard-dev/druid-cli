package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var pushMinRAM string
var pushMinCPU string
var pushMinDisk string
var pushImage string
var pushScrollPorts []string
var pushPackMeta bool
var pushSmart bool
var pushCategory string

var PushCommand = &cobra.Command{
	Use:   "push [artifact] [dir]",
	Short: "Generate OCI Artifacts and push to a remote registry",
	Args:  cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		credStore := loadRegistryStore()

		fullPath := currentWorkingDir()
		artifact := ""
		switch len(args) {
		case 1:
			if args[0] != "" {
				if _, err := os.Stat(filepath.Join(args[0], "scroll.yaml")); err == nil {
					fullPath = args[0]
				} else {
					artifact = args[0]
				}
			}
		case 2:
			artifact = args[0]
			fullPath = args[1]
		}

		scroll, err := domain.NewScroll(fullPath)

		if err != nil {
			return err
		}

		repo := scroll.Name
		tag := scroll.AppVersion

		if artifact != "" {
			repo, tag = utils.SplitArtifact(artifact)
		}

		logger.Log().Info("Pushing "+repo+":"+tag+" to registry", zap.String("path", fullPath))

		ociClient := registry.NewOciClient(credStore)

		overrides := map[string]string{}
		if pushMinRAM != "" {
			overrides["gg.druid.scroll.minRam"] = pushMinRAM
		}
		if pushMinCPU != "" {
			overrides["gg.druid.scroll.minCpu"] = pushMinCPU
		}
		if pushMinDisk != "" {
			overrides["gg.druid.scroll.minDisk"] = pushMinDisk
		}
		if pushImage != "" {
			overrides["gg.druid.scroll.image"] = pushImage
		}
		if pushSmart {
			overrides["gg.druid.scroll.smart"] = "true"
		}
		if pushCategory != "" {
			overrides["gg.druid.scroll.category"] = pushCategory
		}
		for _, p := range pushScrollPorts {
			parts := strings.Split(p, "=")
			name := parts[0]
			port := "0"
			if len(parts) == 2 {
				port = parts[1]
			}
			overrides[fmt.Sprintf("gg.druid.scroll.port.%s", name)] = port
		}

		_, err = ociClient.Push(fullPath, repo, tag, overrides, pushPackMeta, &scroll.File)
		if err != nil {
			return err
		}

		logger.Log().Info("Pushed "+scroll.Name+" to registry", zap.String("path", fullPath))
		return nil
	},
}

func init() {
	RootCmd.AddCommand(PushCommand)
	PushCommand.Flags().StringVarP(&pushMinRAM, "min-ram", "r", pushMinRAM, "Minimum RAM required to run the application. (Will be added as a manifest annotation gg.druid.scroll.minRam)")
	PushCommand.Flags().StringVarP(&pushMinCPU, "min-cpu", "c", pushMinCPU, "Minimum CPU required to run the application. (Will be added as a manifest annotation gg.druid.scroll.minCpu)")
	PushCommand.Flags().StringVarP(&pushMinDisk, "min-disk", "d", pushMinDisk, "Minimum Disk required to run the application. (Will be added as a manifest annotation gg.druid.scroll.minDisk)")
	PushCommand.Flags().BoolVarP(&pushSmart, "smart", "s", false, "Indicates, if the scroll is able to run as a smart deployment (Will be added as a manifest annotation gg.druid.scroll.smart)")
	PushCommand.Flags().StringVar(&pushCategory, "category", pushCategory, "Category of the scroll. (Will be added as a manifest annotation gg.druid.scroll.category)")
	PushCommand.Flags().StringVarP(&pushImage, "image", "i", pushImage, "Image to use for the scroll. (Will be added as a manifest annotation gg.druid.scroll.image)")
	PushCommand.Flags().StringSliceVarP(&pushScrollPorts, "port", "p", pushScrollPorts, "Ports to expose. Format webserver=80, dns=53/udp or just ftp (Will be added as a manifest annotation gg.druid.scroll.ports.<name>)")
	PushCommand.Flags().BoolVarP(&pushPackMeta, "pack-meta", "m", pushPackMeta, "Pack the meta folder into the scroll.")
}
