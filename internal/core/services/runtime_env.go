package services

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

type RuntimeEnvContext struct {
	ScrollID   string
	ScrollName string
	Backend    string
	Routing    []domain.RuntimeRouteAssignment
}

func BuildRuntimeProcedureEnv(file *domain.File, commandName string, command *domain.CommandInstructionSet, context RuntimeEnvContext) (map[string]map[string]string, error) {
	if file == nil {
		return nil, fmt.Errorf("scroll file is required")
	}
	if command == nil {
		return nil, fmt.Errorf("command is required")
	}
	base, err := runtimeEnv(file, context)
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string]string, len(command.Procedures))
	for idx, procedure := range command.Procedures {
		if procedure == nil {
			continue
		}
		env := map[string]string{}
		for key, value := range procedure.Env {
			env[key] = value
		}
		for key, value := range base {
			env[key] = value
		}
		result[domain.ProcedureName(commandName, idx, procedure)] = env
	}
	return result, nil
}

func runtimeEnv(file *domain.File, context RuntimeEnvContext) (map[string]string, error) {
	env := map[string]string{}
	if context.ScrollID != "" {
		env["DRUID_SCROLL_ID"] = context.ScrollID
	}
	scrollName := context.ScrollName
	if scrollName == "" {
		scrollName = file.Name
	}
	if scrollName != "" {
		env["DRUID_SCROLL_NAME"] = scrollName
	}
	if context.Backend != "" {
		env["DRUID_RUNTIME_BACKEND"] = context.Backend
	}

	seen := map[string]string{}
	for _, port := range file.Ports {
		suffix := envSuffix(port.Name)
		if suffix == "" {
			return nil, fmt.Errorf("port name is required for runtime env")
		}
		if previous := seen[suffix]; previous != "" {
			return nil, fmt.Errorf("port names %q and %q normalize to the same env name", previous, port.Name)
		}
		seen[suffix] = port.Name
		env["DRUID_PORT_"+suffix] = strconv.Itoa(port.Port)
		env["DRUID_PORT_"+suffix+"_1"] = strconv.Itoa(port.Port)
		if port.Protocol != "" {
			env["DRUID_PORT_"+suffix+"_PROTOCOL"] = port.Protocol
		}
	}

	if len(context.Routing) == 0 {
		env["DRUID_IP_WAIT"] = "true"
		return env, nil
	}
	for _, assignment := range context.Routing {
		portName := assignment.PortName
		if portName == "" {
			portName = assignment.Name
		}
		suffix := envSuffix(portName)
		if suffix == "" {
			continue
		}
		if assignment.ExternalIP != "" {
			env["DRUID_PORT_"+suffix+"_IP"] = assignment.ExternalIP
			if env["DRUID_IP"] == "" {
				env["DRUID_IP"] = assignment.ExternalIP
			}
		}
		if assignment.PublicPort > 0 {
			env["DRUID_PORT_"+suffix+"_PUBLIC"] = strconv.Itoa(assignment.PublicPort)
		}
		if assignment.Host != "" {
			env["DRUID_PORT_"+suffix+"_HOST"] = assignment.Host
		}
		if assignment.URL != "" {
			env["DRUID_PORT_"+suffix+"_URL"] = assignment.URL
		}
	}
	return env, nil
}

func envSuffix(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
			lastUnderscore = false
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.TrimRight(b.String(), "_")
}
