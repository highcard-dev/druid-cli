package services

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func resolveRuntimePorts(ports []domain.Port, routing []domain.RuntimeRouteAssignment, requireAssignments bool) ([]domain.Port, error) {
	resolved := append([]domain.Port(nil), ports...)
	for index := range resolved {
		port := &resolved[index]
		if port.Port != 0 {
			continue
		}

		assignedPort := 0
		for _, assignment := range routing {
			portName := assignment.PortName
			if portName == "" {
				portName = assignment.Name
			}
			if portName != port.Name {
				continue
			}
			if assignment.PublicPort < 1 || assignment.PublicPort > 65535 {
				return nil, fmt.Errorf("dynamic port %q has invalid public port %d", port.Name, assignment.PublicPort)
			}
			if assignedPort != 0 && assignedPort != assignment.PublicPort {
				return nil, fmt.Errorf("dynamic port %q has conflicting public ports %d and %d", port.Name, assignedPort, assignment.PublicPort)
			}
			assignedPort = assignment.PublicPort
		}

		if assignedPort == 0 {
			if requireAssignments {
				return nil, fmt.Errorf("dynamic port %q has no public routing assignment", port.Name)
			}
			continue
		}
		port.Port = assignedPort
	}
	return resolved, nil
}

func validateDynamicPortsUnchanged(
	ports []domain.Port,
	currentRouting []domain.RuntimeRouteAssignment,
	nextRouting []domain.RuntimeRouteAssignment,
) error {
	currentPorts, err := resolveRuntimePorts(ports, currentRouting, false)
	if err != nil {
		return err
	}
	nextPorts, err := resolveRuntimePorts(ports, nextRouting, false)
	if err != nil {
		return err
	}
	for index, port := range ports {
		if port.Port != 0 || currentPorts[index].Port == nextPorts[index].Port {
			continue
		}
		return fmt.Errorf(
			"cannot change dynamic port %q from %d to %d while runtime is running; stop the runtime before applying routing",
			port.Name,
			currentPorts[index].Port,
			nextPorts[index].Port,
		)
	}
	return nil
}
