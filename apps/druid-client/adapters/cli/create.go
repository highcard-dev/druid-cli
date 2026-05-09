package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/highcard-dev/daemon/apps/druid-client/adapters/daemon"
	"github.com/highcard-dev/daemon/internal/core/domain"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/spf13/cobra"
)

func (a *App) createCmd() *cobra.Command {
	var stateDir string
	var scrollRoot string
	var dataRoot string
	var noData bool
	cmd := &cobra.Command{
		Use:   "create <artifact-or-path> [name]",
		Short: "Create a scroll through the daemon",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			artifact := args[0]
			name := ""
			if len(args) == 2 {
				name = args[1]
			}
			if stateDir == "" {
				defaultStateDir, err := utils.DefaultRuntimeStateDir()
				if err != nil {
					return err
				}
				stateDir = defaultStateDir
			}
			if (scrollRoot == "") != (dataRoot == "") {
				return fmt.Errorf("--scroll-root and --data-root must be provided together")
			}

			service, err := a.runtimeService()
			if err != nil {
				return err
			}

			if scrollRoot != "" {
				if err := coreservices.MaterializeScrollArtifact(artifact, scrollRoot, dataRoot, registry.NewOciClient(a.loadRegistryStore()), !noData); err != nil {
					return err
				}
			} else {
				if !localArtifactExists(artifact) {
					scroll, err := service.Create(cmd.Context(), name, artifact, "", "")
					if err == nil {
						return printJSON(scroll)
					}
					if !errors.Is(err, daemon.ErrMaterializationUnsupported) {
						return err
					}
				}
				store := coreservices.NewRuntimeStateStore(stateDir)
				tmpParent := filepath.Join(stateDir, "tmp")
				if err := os.MkdirAll(tmpParent, 0755); err != nil {
					return err
				}
				tmpDir, err := os.MkdirTemp(tmpParent, "create-scroll-*")
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmpDir)

				stagedScrollRoot := filepath.Join(tmpDir, "spec")
				stagedDataRoot := filepath.Join(tmpDir, "data")
				if err := coreservices.MaterializeScrollArtifact(artifact, stagedScrollRoot, stagedDataRoot, registry.NewOciClient(a.loadRegistryStore()), !noData); err != nil {
					return err
				}
				stagedScroll, err := domain.NewScroll(stagedScrollRoot)
				if err != nil {
					return err
				}
				id, err := coreservices.RuntimeScrollID(name, stagedScroll.Name)
				if err != nil {
					return err
				}
				scrollRoot = store.ScrollRoot(id)
				dataRoot = store.DataRoot(id)
				if err := coreservices.MoveMaterializedScroll(stagedScrollRoot, stagedDataRoot, scrollRoot, dataRoot); err != nil {
					return err
				}
			}

			scroll, err := service.Create(cmd.Context(), name, artifact, scrollRoot, dataRoot)
			if err != nil {
				return err
			}
			return printJSON(scroll)
		},
	}
	cmd.Flags().StringVar(&stateDir, "state-dir", "", "Runtime state directory for local materialization (default: ~/.druid/runtime)")
	cmd.Flags().StringVar(&scrollRoot, "scroll-root", "", "Daemon-local path containing materialized scroll spec")
	cmd.Flags().StringVar(&dataRoot, "data-root", "", "Daemon-local path containing runtime data")
	cmd.Flags().BoolVar(&noData, "no-data", false, "Skip scroll data files")
	return cmd
}

func localArtifactExists(artifact string) bool {
	_, err := os.Stat(artifact)
	return err == nil
}
