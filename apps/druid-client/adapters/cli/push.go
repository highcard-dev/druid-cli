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

func (a *App) pushCmd() *cobra.Command {
	var minRam string
	var minCpu string
	var minDisk string
	var image string
	var scrollPorts []string
	var packMeta bool
	var smart bool
	var category string

	cmd := &cobra.Command{
		Use:   "push [artifact] [dir]",
		Short: "Generate OCI Artifacts and push to a remote registry",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			credStore := a.loadRegistryStore()
			if !credStore.HasCredentials() {
				return fmt.Errorf("no registry credentials configured. Please use `druid-client login` to set them")
			}

			fullPath := currentWorkingDir()
			artifact := ""
			switch len(args) {
			case 1:
				if isScrollDir(args[0]) {
					fullPath = args[0]
				} else {
					artifact = args[0]
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
			if category != "" {
				overrides["gg.druid.scroll.category"] = category
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

			_, err = ociClient.Push(fullPath, repo, tag, overrides, packMeta, &scroll.File)
			if err != nil {
				return err
			}

			logger.Log().Info("Pushed "+scroll.Name+" to registry", zap.String("path", fullPath))
			return nil
		},
	}

	cmd.AddCommand(a.pushCategoryCmd())

	cmd.Flags().StringVarP(&minRam, "min-ram", "r", minRam, "Minimum RAM required to run the application. (Will be added as a manifest annotation gg.druid.scroll.minRam)")
	cmd.Flags().StringVarP(&minCpu, "min-cpu", "c", minCpu, "Minimum CPU required to run the application. (Will be added as a manifest annotation gg.druid.scroll.minCpu)")
	cmd.Flags().StringVarP(&minDisk, "min-disk", "d", minDisk, "Minimum Disk required to run the application. (Will be added as a manifest annotation gg.druid.scroll.minDisk)")
	cmd.Flags().BoolVarP(&smart, "smart", "s", false, "Indicates, if the scroll is able to run as a smart deployment (Will be added as a manifest annotation gg.druid.scroll.smart)")
	cmd.Flags().StringVar(&category, "category", category, "Category of the scroll. (Will be added as a manifest annotation gg.druid.scroll.category)")

	cmd.Flags().StringVarP(&image, "image", "i", image, "Image to use for the scroll. (Will be added as a manifest annotation gg.druid.scroll.image)")

	cmd.Flags().StringSliceVarP(&scrollPorts, "port", "p", scrollPorts, "Ports to expose. Format webserver=80, dns=53/udp or just ftp (Will be added as a manifest annotation gg.druid.scroll.ports.<name>)")

	cmd.Flags().BoolVarP(&packMeta, "pack-meta", "m", packMeta, "Pack the meta folder into the scroll.")
	return cmd
}

func isScrollDir(dir string) bool {
	if dir == "" {
		return false
	}
	path := filepath.Join(dir, "scroll.yaml")
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}
