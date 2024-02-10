package cmd

import (
	"fmt"
	"os"

	semver "github.com/Masterminds/semver/v3"
	"github.com/highcard-dev/daemon/internal/core/services"
	logger "github.com/highcard-dev/daemon/internal/core/services/log"
	"github.com/spf13/cobra"
)

var SemverCmd = &cobra.Command{
	Use:   "app_version [semver1 string] [lt|gt|eq|ne|le|ge] [semver2 string]",
	Short: "Show or compare active app version",
	Long:  "This command shows the active app version. If a comparison operator and semver string are provided, the command will compare the active app version to the semver string. If the comparison is true, the command will exit with a 0 exit code. If the comparison is false, the command will exit with a 1 exit code.",
	Args:  cobra.MaximumNArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger.Log()

		scrollService := services.NewScrollService(cwd)

		scroll, err := scrollService.LoadScrollWithLockfile()

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

		var compare string
		var semver1, semver2 *semver.Version

		if len(args) == 2 {
			compare = args[0]
			semver1 = semverAppVersion
			semver2, err = semver.NewVersion(args[1])
			if err != nil {
				return fmt.Errorf("error parsing application version (argument 2): %w", err)
			}
		} else {
			compare = args[1]
			semver1, err = semver.NewVersion(args[0])
			if err != nil {
				return fmt.Errorf("error parsing application version (argument 1): %w", err)
			}
			semver2, err = semver.NewVersion(args[2])
			if err != nil {
				return fmt.Errorf("error parsing application version (argument 3): %w", err)
			}

		}

		if compare == "eq" {
			if semver1.Equal(semver2) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "lt" {
			if semver1.LessThan(semver2) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "gt" {
			if semver1.GreaterThan(semver2) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "ne" {
			if !semver1.Equal(semver2) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "le" {
			if semver1.LessThan(semver2) || semver1.Equal(semver2) {
				return nil
			} else {
				os.Exit(1)
			}
		}
		if compare == "ge" {
			if semver1.GreaterThan(semver2) || semver1.Equal(semver2) {
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
