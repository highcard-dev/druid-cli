package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/highcard-dev/daemon/internal/api"
	"github.com/spf13/cobra"
)

func (a *App) routingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "routing",
		Short: "Inspect or apply daemon runtime routing",
	}
	cmd.AddCommand(a.routingTargetsCmd())
	cmd.AddCommand(a.routingApplyCmd())
	return cmd
}

func (a *App) routingTargetsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "targets <name>",
		Short: "Show backend service targets for a scroll",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := a.runtimeService()
			if err != nil {
				return err
			}
			targets, err := service.RoutingTargets(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(targets)
		},
	}
}

func (a *App) routingApplyCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "apply <name>",
		Short: "Persist assigned public routing for a scroll",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return fmt.Errorf("--file is required")
			}
			payload, err := readRoutingAssignments(file)
			if err != nil {
				return err
			}
			service, err := a.runtimeService()
			if err != nil {
				return err
			}
			scroll, err := service.ApplyRouting(cmd.Context(), args[0], payload.Assignments)
			if err != nil {
				return err
			}
			return printJSON(scroll)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "JSON file with an assignments array, or '-' for stdin")
	return cmd
}

type routingAssignmentsPayload struct {
	Assignments []api.RuntimeRouteAssignment `json:"assignments"`
}

func readRoutingAssignments(file string) (routingAssignmentsPayload, error) {
	var data []byte
	var err error
	if file == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(file)
	}
	if err != nil {
		return routingAssignmentsPayload{}, err
	}
	var payload routingAssignmentsPayload
	if err := json.Unmarshal(data, &payload); err == nil && payload.Assignments != nil {
		return payload, nil
	}
	var assignments []api.RuntimeRouteAssignment
	if err := json.Unmarshal(data, &assignments); err != nil {
		return routingAssignmentsPayload{}, err
	}
	return routingAssignmentsPayload{Assignments: assignments}, nil
}
