package routing

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/highcard-dev/daemon/internal/api"
)

func AssignmentsFromPublishes(publishes []string, targets []api.RuntimeRoutingTarget, id string) ([]api.RuntimeRouteAssignment, error) {
	assignments := make([]api.RuntimeRouteAssignment, 0, len(publishes))
	for _, publish := range publishes {
		externalIP := "127.0.0.1"
		parts := strings.Split(publish, ":")
		if len(parts) == 3 {
			externalIP = parts[0]
			parts = parts[1:]
		}
		if len(parts) != 2 || externalIP == "" || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid publish %q, want [external-ip:]public-port:target[/protocol]", publish)
		}
		publicPort, err := strconv.Atoi(parts[0])
		if err != nil || publicPort < 1 || publicPort > 65535 {
			return nil, fmt.Errorf("invalid public port in publish %q", publish)
		}
		targetName, protocolOverride, hasProtocolOverride := strings.Cut(parts[1], "/")
		if targetName == "" || (hasProtocolOverride && protocolOverride == "") {
			return nil, fmt.Errorf("invalid target in publish %q", publish)
		}

		target, err := targetForPublish(targetName, targets, id)
		if err != nil {
			return nil, err
		}
		protocol := target.Protocol
		if hasProtocolOverride {
			protocol = protocolOverride
		}
		if protocol == "" {
			protocol = "tcp"
		}

		host := "localhost"
		name := target.Name
		portName := target.PortName
		publicPortValue := publicPort
		assignment := api.RuntimeRouteAssignment{
			Name:       &name,
			PortName:   &portName,
			ExternalIp: &externalIP,
			PublicPort: &publicPortValue,
			Host:       &host,
			Protocol:   &protocol,
		}
		if protocol == "http" || protocol == "https" {
			url := fmt.Sprintf("%s://%s:%d", protocol, host, publicPort)
			assignment.Url = &url
		}
		assignments = append(assignments, assignment)
	}
	return assignments, nil
}

func targetForPublish(targetName string, targets []api.RuntimeRoutingTarget, id string) (api.RuntimeRoutingTarget, error) {
	matches := make([]api.RuntimeRoutingTarget, 0, 1)
	for _, target := range targets {
		if target.PortName == targetName {
			matches = append(matches, target)
		}
	}
	if len(matches) == 0 {
		if targetPort, err := strconv.Atoi(targetName); err == nil {
			for _, target := range targets {
				if target.Port == targetPort {
					matches = append(matches, target)
				}
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	command := "druid routing targets <name>"
	if id != "" {
		command = "druid routing targets " + id
	}
	if len(matches) == 0 {
		return api.RuntimeRoutingTarget{}, fmt.Errorf("routing target %q not found; run %q", targetName, command)
	}
	return api.RuntimeRoutingTarget{}, fmt.Errorf("routing target %q is ambiguous; run %q", targetName, command)
}
