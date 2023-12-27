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
			print(scroll.AppVersion)
			return nil
		}

		semverAppVersion, err := semver.NewVersion(scroll.AppVersion)

		if err != nil {
			return fmt.Errorf("error parsing application version as semver: %w", err)
		}

		// If one arg, check if it's equal to the version
		if len(args) == 1 {
			compareVersionSemverString := args[0]
			compareVersionSemver, err := semver.NewVersion(compareVersionSemverString)
			if err != nil {
				return fmt.Errorf("error parsing application version: %w", err)
			}
			if semverAppVersion.Equal(compareVersionSemver) {
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
			if semverAppVersion.Equal(compareVersionSemver) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "lt" {
			if semverAppVersion.LessThan(compareVersionSemver) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "gt" {
			if semverAppVersion.GreaterThan(compareVersionSemver) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "ne" {
			if !semverAppVersion.Equal(compareVersionSemver) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "le" {
			if semverAppVersion.LessThan(compareVersionSemver) || semverAppVersion.Equal(compareVersionSemver) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "ge" {
			if semverAppVersion.GreaterThan(compareVersionSemver) || semverAppVersion.Equal(compareVersionSemver) {
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
