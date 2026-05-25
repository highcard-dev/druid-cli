package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/highcard-dev/daemon/internal/api"
	routingutil "github.com/highcard-dev/daemon/internal/routing"
	"github.com/spf13/cobra"
)

var routingApplyFile string
var routingApplyPublishes []string

var RoutingApplyCommand = &cobra.Command{
	Use:   "apply <name>",
	Short: "Persist assigned public routing for a scroll",
	Example: `  druid routing apply my-scroll -p 8080:http
  druid routing apply my-scroll -p 8080:80
  druid routing apply my-scroll -p 127.0.0.1:8080:http
  druid routing apply my-scroll -p 8443:http/https
  druid routing apply my-scroll --file routing.json
  cat routing.json | druid routing apply my-scroll --file -`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		assignments, err := routingAssignmentsForApply(cmd.Context(), daemon, args[0], routingApplyFile, routingApplyPublishes)
		if err != nil {
			return err
		}
		scroll, err := daemon.ApplyScrollRouting(cmd.Context(), args[0], assignments)
		if err != nil {
			return err
		}
		return printJSON(scroll)
	},
}

func init() {
	RoutingApplyCommand.Flags().StringVarP(&routingApplyFile, "file", "f", "", "JSON file with an assignments array, or '-' for stdin")
	RoutingApplyCommand.Flags().StringArrayVarP(&routingApplyPublishes, "publish", "p", nil, "Publish routing as [external-ip:]public-port:target[/protocol]")
}

func routingAssignmentsForApply(ctx context.Context, daemon RuntimeDaemon, id string, file string, publishes []string) ([]api.RuntimeRouteAssignment, error) {
	if file != "" && len(publishes) > 0 {
		return nil, fmt.Errorf("--file and --publish cannot be used together")
	}
	if len(publishes) > 0 {
		targets, err := daemon.GetScrollRoutingTargets(ctx, id)
		if err != nil {
			return nil, err
		}
		return routingutil.AssignmentsFromPublishes(publishes, targets, id)
	}
	if file == "" {
		return nil, fmt.Errorf("--file or --publish is required")
	}
	data, err := readRoutingAssignmentsFile(file)
	if err != nil {
		return nil, err
	}
	return parseRoutingAssignments(data)
}

func readRoutingAssignmentsFile(file string) ([]byte, error) {
	if file == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read routing assignments from stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read routing assignments file: %w", err)
	}
	return data, nil
}

func parseRoutingAssignments(data []byte) ([]api.RuntimeRouteAssignment, error) {
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		var request api.ApplyRoutingRequest
		if err := json.Unmarshal(data, &request); err != nil {
			return nil, fmt.Errorf("failed to unmarshal routing assignments: %w", err)
		}
		return request.Assignments, nil
	}
	var assignments []api.RuntimeRouteAssignment
	if err := json.Unmarshal(data, &assignments); err != nil {
		return nil, fmt.Errorf("failed to unmarshal routing assignments: %w", err)
	}
	return assignments, nil
}
