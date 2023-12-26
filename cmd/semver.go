package cmd

import (
	"fmt"
	"os"

	semver "github.com/Masterminds/semver/v3"
	"github.com/highcard-dev/daemon/internal/core/services"
	logger "github.com/highcard-dev/daemon/internal/core/services/log"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var SemverCmd = &cobra.Command{
	Use:   "app_version [semver1 string] [lt|gt|eq|ne|le|ge] [semver2 string]",
	Short: "Show or compare active app version",
	Long:  "This command shows the active app version. If a comparison operator and semver string are provided, the command will compare the active app version to the semver string. If the comparison is true, the command will exit with a 0 exit code. If the comparison is false, the command will exit with a 1 exit code.",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger.Log()
		host := viper.GetString("registry.host")
		user := viper.GetString("registry.user")
		password := viper.GetString("registry.password")

		client := registry.NewOciClient(host, user, password)

		logManager := services.NewLogManager()
		hub := services.NewHub()
		processManager := services.NewProcessManager(logManager, hub)
		scrollService := services.NewScrollService(cwd, client, logManager, processManager, hub, services.NewPluginManager())

		scroll, err := scrollService.Load(ignoreVersionCheck)

		if err != nil {
			return fmt.Errorf("error loading scroll: %w", err)
		}

		// If no args, just print the version
		if len(args) == 0 {
			print(scroll.AppVersion.String())
			return nil
		}

		// If one arg, check if it's equal to the version
		if len(args) == 1 {
			compareVersionSemverString := args[0]
			compareVersionSemver, err := semver.NewVersion(compareVersionSemverString)
			if err != nil {
				return fmt.Errorf("error parsing application version: %w", err)
			}
			if scroll.AppVersion.Equal(compareVersionSemver) {
				return nil
			} else {
				os.Exit(1)
			}
		}

		// If two args, check the comparison
		compare := args[0]
		compareVersionSemverString := args[1]

		compareVersionSemver, err := semver.NewVersion(compareVersionSemverString)
		if err != nil {
			return fmt.Errorf("error parsing application version: %w", err)
		}

		if args[0] == "eq" || len(args) == 1 {
			if scroll.AppVersion.Equal(compareVersionSemver) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "lt" {
			if scroll.AppVersion.LessThan(compareVersionSemver) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "gt" {
			if scroll.AppVersion.GreaterThan(compareVersionSemver) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "ne" {
			if !scroll.AppVersion.Equal(compareVersionSemver) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "le" {
			if scroll.AppVersion.LessThan(compareVersionSemver) || scroll.AppVersion.Equal(compareVersionSemver) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "ge" {
			if scroll.AppVersion.GreaterThan(compareVersionSemver) || scroll.AppVersion.Equal(compareVersionSemver) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		return fmt.Errorf("invalid comparison operator: %s", compare)
	},
}

func init() {
}
