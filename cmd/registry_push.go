package cmd

import (
	"fmt"
	"path"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
	logger "github.com/highcard-dev/daemon/internal/core/services/log"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var minRam string
var minCpu string
var minDisk string
var image string
var ports []string

var PushCommand = &cobra.Command{
	Use:   "push",
	Short: "Generate OCI Artifacts and push to a remote registry",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

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

		fullPath := path.Join(cwd, folder)

		scroll, err := domain.NewScroll(fullPath)

		if err != nil {
			return err
		}

		ociClient := registry.NewOciClient(host, user, password)

		repo := scroll.Name

		tag := scroll.AppVersion

		ps := make(map[string]string, len(ports))

		for _, p := range ports {

			parts := strings.Split(p, "=")
			name := parts[0]
			port := "0"
			if len(parts) == 2 {
				port = parts[1]
			}
			ps[name] = port
		}

		_, err = ociClient.Push(fullPath, repo, tag, registry.AnnotationInfo{
			MinRam:  minRam,
			MinCpu:  minCpu,
			MinDisk: minDisk,
			Image:   image,
			Ports:   ps,
		})

		if err != nil {
			return err
		}

		logger.Log().Info("Pushed to registry")
		return nil
	},
}

func init() {
	PushCommand.AddCommand(PushMetaCommand)

	PushCommand.Flags().StringVarP(&minRam, "min-ram", "r", minRam, "Minimum RAM required to run the application. (Will be added as a manifest annotation gg.druid.scroll.minRam)")
	PushCommand.Flags().StringVarP(&minCpu, "min-cpu", "c", minCpu, "Minimum CPU required to run the application. (Will be added as a manifest annotation gg.druid.scroll.minCpu)")
	PushCommand.Flags().StringVarP(&minDisk, "min-disk", "d", minDisk, "Minimum Disk required to run the application. (Will be added as a manifest annotation gg.druid.scroll.minDisk)")

	PushCommand.Flags().StringVarP(&image, "image", "i", image, "Image to use for the scroll. (Will be added as a manifest annotation gg.druid.scroll.image)")

	PushCommand.Flags().StringSliceVarP(&ports, "port", "p", ports, "Ports to expose. Format webserver=80, dns=53/udp or just ftp (Will be added as a manifest annotation gg.druid.scroll.ports.<name>)")
}
