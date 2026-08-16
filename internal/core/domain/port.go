package domain

import (
	"fmt"
	"regexp"
	"strings"
)

var portIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func ValidateFixedPorts(ports []Port) error {
	names := make(map[string]struct{}, len(ports))
	bindings := make(map[string]struct{}, len(ports))
	for _, port := range ports {
		if strings.TrimSpace(port.Name) == "" || !portIdentifier.MatchString(port.Name) {
			return fmt.Errorf("invalid port name %q", port.Name)
		}
		if port.Port < 1 || port.Port > 65535 {
			return fmt.Errorf("port %s has invalid port %d", port.Name, port.Port)
		}
		switch port.Protocol {
		case "http", "https", "tcp", "udp":
		default:
			return fmt.Errorf("port %s has invalid protocol %q", port.Name, port.Protocol)
		}
		if _, exists := names[port.Name]; exists {
			return fmt.Errorf("port name %q is duplicated", port.Name)
		}
		names[port.Name] = struct{}{}
		binding := fmt.Sprintf("%d/%s", port.Port, port.Protocol)
		if _, exists := bindings[binding]; exists {
			return fmt.Errorf("port %s duplicates %s", port.Name, binding)
		}
		bindings[binding] = struct{}{}
	}
	return nil
}
