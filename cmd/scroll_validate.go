package cmd

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/spf13/cobra"
)

var ScrollValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validates the scroll file",
	Long:  `This command validates the scroll file to ensure it meets the required criteria.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		scrollDir := cwd
		if len(args) > 0 {
			scrollDir = args[0]
		}

		scroll, err := domain.NewScroll(scrollDir)

		if err != nil {
			return fmt.Errorf("failed to load scroll: %w", err)
		}

		if err := scroll.Validate(); err != nil {
			return fmt.Errorf("failed to validate scroll: %w", err)
		}

		fmt.Println("Scroll validated successfully.")
		return nil
	},
}
