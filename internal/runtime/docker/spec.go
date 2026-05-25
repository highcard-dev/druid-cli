package docker

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
	"github.com/highcard-dev/daemon/internal/core/domain"
)

func containerSpec(commandName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, routing []domain.RuntimeRouteAssignment, env map[string]string) (*container.Config, *container.HostConfig, error) {
	if procedure.Image == "" {
		return nil, nil, errors.New("docker image is required")
	}

	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}
	globalPortsByName := portsByName(globalPorts)
	for _, expectedPort := range procedure.ExpectedPorts {
		port, ok := globalPortsByName[expectedPort.Name]
		if !ok {
			return nil, nil, fmt.Errorf("expected port %s is not defined in top-level ports", expectedPort.Name)
		}
		dockerPort := dockerRuntimePort(port.Port, port.Protocol)
		exposedPorts[dockerPort] = struct{}{}
		if assignment, ok := routeAssignmentForPort(expectedPort.Name, routing); ok && assignment.PublicPort > 0 {
			dockerPort = dockerRuntimePort(port.Port, firstNonEmpty(assignment.Protocol, port.Protocol))
			exposedPorts[dockerPort] = struct{}{}
			portBindings[dockerPort] = []nat.PortBinding{{
				HostIP:   assignment.ExternalIP,
				HostPort: fmt.Sprintf("%d", assignment.PublicPort),
			}}
		}
	}

	mounts := []mount.Mount{}
	for _, mount := range procedure.Mounts {
		if mount.Path == "" {
			return nil, nil, fmt.Errorf("mount path is required")
		}
		dockerMount, err := DockerMount(root, mount.Path, mount.ReadOnly, procedureDataSubPath(mount.SubPath))
		if err != nil {
			return nil, nil, err
		}
		mounts = append(mounts, dockerMount)
	}

	return &container.Config{
			Image:        procedure.Image,
			Cmd:          procedure.Command,
			WorkingDir:   procedure.WorkingDir,
			Env:          envArgs(env),
			ExposedPorts: exposedPorts,
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			OpenStdin:    true,
			Tty:          procedure.TTY,
			Labels: map[string]string{
				"druid.command":   commandName,
				"druid.root-hash": rootHash(root),
			},
		}, &container.HostConfig{
			Mounts:       mounts,
			PortBindings: portBindings,
			ExtraHosts:   dockerExtraHosts(),
		}, nil
}

func routeAssignmentForPort(portName string, routing []domain.RuntimeRouteAssignment) (domain.RuntimeRouteAssignment, bool) {
	for _, assignment := range routing {
		if assignment.PortName == portName || assignment.Name == portName {
			return assignment, true
		}
	}
	return domain.RuntimeRouteAssignment{}, false
}

func dockerRuntimePort(port int, protocol string) nat.Port {
	if protocol == "" || protocol == "http" || protocol == "https" {
		protocol = "tcp"
	}
	return nat.Port(fmt.Sprintf("%d/%s", port, protocol))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func procedureDataSubPath(subPath string) string {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(subPath, "/")))
	if subPath == "" {
		return domain.RuntimeDataDir
	}
	if clean == "." {
		return "."
	}
	if clean == domain.RuntimeDataDir || strings.HasPrefix(clean, domain.RuntimeDataDir+"/") {
		return clean
	}
	return filepath.ToSlash(filepath.Join(domain.RuntimeDataDir, filepath.FromSlash(clean)))
}

func envArgs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys))
	for _, key := range keys {
		args = append(args, fmt.Sprintf("%s=%s", key, env[key]))
	}
	return args
}

type ContainerSpec struct {
	Image        string
	Command      []string
	WorkingDir   string
	Env          []string
	Mounts       []mount.Mount
	PortBindings nat.PortMap
	TTY          bool
}

func BuildContainerSpec(commandName string, procedure *domain.Procedure, root string, globalPorts []domain.Port) (*ContainerSpec, error) {
	return BuildContainerSpecWithEnv(commandName, procedure, root, globalPorts, procedure.Env)
}

func BuildContainerSpecWithEnv(commandName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, env map[string]string) (*ContainerSpec, error) {
	return BuildContainerSpecWithRouting(commandName, procedure, root, globalPorts, nil, env)
}

func BuildContainerSpecWithRouting(commandName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, routing []domain.RuntimeRouteAssignment, env map[string]string) (*ContainerSpec, error) {
	config, hostConfig, err := containerSpec(commandName, procedure, root, globalPorts, routing, env)
	if err != nil {
		return nil, err
	}
	return &ContainerSpec{
		Image:        config.Image,
		Command:      config.Cmd,
		WorkingDir:   config.WorkingDir,
		Env:          config.Env,
		Mounts:       hostConfig.Mounts,
		PortBindings: hostConfig.PortBindings,
		TTY:          config.Tty,
	}, nil
}
