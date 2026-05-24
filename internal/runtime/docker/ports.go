package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/highcard-dev/daemon/internal/core/domain"
)

func (b *Backend) ExpectedPorts(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimePortStatus, error) {
	statuses := []domain.RuntimePortStatus{}
	portsByName := portsByName(globalPorts)
	for commandName, command := range commands {
		if command == nil {
			continue
		}
		for idx, procedure := range command.Procedures {
			if procedure == nil || len(procedure.ExpectedPorts) == 0 {
				continue
			}
			procedureName := fmt.Sprintf("%s.%d", commandName, idx)
			if procedure.Id != nil {
				procedureName = *procedure.Id
			}
			containerStatuses, err := b.expectedPortsForProcedure(root, procedureName, procedure, portsByName)
			if err != nil {
				return nil, err
			}
			statuses = append(statuses, containerStatuses...)
		}
	}
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].Procedure == statuses[j].Procedure {
			return statuses[i].Name < statuses[j].Name
		}
		return statuses[i].Procedure < statuses[j].Procedure
	})
	return statuses, nil
}

func (b *Backend) RoutingTargets(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimeRoutingTarget, error) {
	portsByName := portsByName(globalPorts)
	targets := []domain.RuntimeRoutingTarget{{
		Name:        "webdav",
		Procedure:   "dev",
		PortName:    "webdav",
		Port:        8084,
		Protocol:    "https",
		ServiceName: ContainerName(root, "dev"),
		ServicePort: 8084,
	}}
	seen := map[string]struct{}{"webdav": {}}
	commandNames := make([]string, 0, len(commands))
	for commandName := range commands {
		commandNames = append(commandNames, commandName)
	}
	sort.Strings(commandNames)
	for _, commandName := range commandNames {
		command := commands[commandName]
		if command == nil {
			continue
		}
		for idx, procedure := range command.Procedures {
			if procedure == nil || len(procedure.ExpectedPorts) == 0 {
				continue
			}
			procedureName := domain.ProcedureName(commandName, idx, procedure)
			serviceName := ContainerName(root, procedureResourceName(commandName, idx))
			for _, expectedPort := range procedure.ExpectedPorts {
				if _, ok := seen[expectedPort.Name]; ok {
					continue
				}
				port, ok := portsByName[expectedPort.Name]
				if !ok {
					return nil, fmt.Errorf("expected port %s is not defined in top-level ports", expectedPort.Name)
				}
				seen[expectedPort.Name] = struct{}{}
				targets = append(targets, domain.RuntimeRoutingTarget{
					Name:        expectedPort.Name,
					Procedure:   procedureName,
					PortName:    expectedPort.Name,
					Port:        port.Port,
					Protocol:    normalizeProtocol(port.Protocol),
					ServiceName: serviceName,
					ServicePort: port.Port,
				})
			}
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	return targets, nil
}

type containerTraffic struct {
	containerID    string
	rxBytes        uint64
	txBytes        uint64
	lastDeltaRX    uint64
	lastActivityAt *time.Time
	samples        []trafficSample
}

type trafficSample struct {
	at time.Time
	rx uint64
	tx uint64
}

type trafficStore struct {
	mu             sync.Mutex
	samples        map[string][]trafficSample
	lastActivityAt map[string]time.Time
}

var globalTrafficStore = &trafficStore{
	samples:        map[string][]trafficSample{},
	lastActivityAt: map[string]time.Time{},
}

func (s *trafficStore) record(containerID string, rxBytes uint64, txBytes uint64, now time.Time) containerTraffic {
	s.mu.Lock()
	defer s.mu.Unlock()
	samples := s.samples[containerID]
	var lastDeltaRX uint64
	if len(samples) > 0 && rxBytes >= samples[len(samples)-1].rx {
		lastDeltaRX = rxBytes - samples[len(samples)-1].rx
		if lastDeltaRX > 0 {
			s.lastActivityAt[containerID] = now
		}
	}
	samples = append(samples, trafficSample{at: now, rx: rxBytes, tx: txBytes})
	cutoff := now.Add(-24 * time.Hour)
	keepFrom := 0
	for keepFrom < len(samples) && samples[keepFrom].at.Before(cutoff) {
		keepFrom++
	}
	samples = samples[keepFrom:]
	s.samples[containerID] = samples

	var lastActivityAt *time.Time
	if last, ok := s.lastActivityAt[containerID]; ok {
		lastCopy := last
		lastActivityAt = &lastCopy
	}
	samplesCopy := append([]trafficSample(nil), samples...)
	return containerTraffic{
		containerID:    containerID,
		rxBytes:        rxBytes,
		txBytes:        txBytes,
		lastDeltaRX:    lastDeltaRX,
		lastActivityAt: lastActivityAt,
		samples:        samplesCopy,
	}
}

func (t containerTraffic) rxDelta(window time.Duration, now time.Time) uint64 {
	if window <= 0 || len(t.samples) == 0 {
		return t.lastDeltaRX
	}
	cutoff := now.Add(-window)
	base := t.samples[0]
	for _, sample := range t.samples {
		if !sample.at.Before(cutoff) {
			base = sample
			break
		}
	}
	if t.rxBytes < base.rx {
		return 0
	}
	return t.rxBytes - base.rx
}

func (b *Backend) expectedPortsForProcedure(root string, procedureName string, procedure *domain.Procedure, ports map[string]domain.Port) ([]domain.RuntimePortStatus, error) {
	statuses := make([]domain.RuntimePortStatus, 0, len(procedure.ExpectedPorts))
	containerName := ContainerName(root, procedureName)
	ctx := context.Background()
	inspected, err := b.client.ContainerInspect(ctx, containerName)
	containerFound := err == nil
	if err != nil && !cerrdefs.IsNotFound(err) {
		return nil, err
	}

	var traffic *containerTraffic
	if containerFound {
		if sample, err := b.containerTraffic(ctx, inspected.ID); err == nil {
			traffic = sample
		}
	}

	for _, expectedPort := range procedure.ExpectedPorts {
		port, ok := ports[expectedPort.Name]
		if !ok {
			return nil, fmt.Errorf("expected port %s is not defined in top-level ports", expectedPort.Name)
		}
		status := domain.RuntimePortStatus{
			Name:             expectedPort.Name,
			Procedure:        procedureName,
			Port:             port.Port,
			Protocol:         normalizeProtocol(port.Protocol),
			KeepAliveTraffic: expectedPort.KeepAliveTraffic,
			Source:           "docker-container-stats",
		}
		if containerFound {
			status.Bound, status.HostIP, status.HostPort = dockerPortBinding(inspected.NetworkSettings.Ports, port)
		}
		if traffic != nil {
			rx := traffic.rxBytes
			tx := traffic.txBytes
			status.RXBytes = &rx
			status.TXBytes = &tx
			status.LastActivityAt = traffic.lastActivityAt
			delta := traffic.lastDeltaRX
			if expectedPort.KeepAliveTraffic != "" {
				threshold, err := domain.ParseKeepAliveTraffic(expectedPort.KeepAliveTraffic)
				if err != nil {
					return nil, err
				}
				delta = traffic.rxDelta(threshold.Window, time.Now())
				trafficOK := delta >= threshold.Bytes
				status.TrafficOK = &trafficOK
				status.TrafficWindow = threshold.Window.String()
			}
			status.Traffic = delta > 0
			status.TrafficBytes = &delta
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (b *Backend) containerTraffic(ctx context.Context, containerID string) (*containerTraffic, error) {
	stats, err := b.client.ContainerStats(ctx, containerID, false)
	if err != nil {
		return nil, err
	}
	defer stats.Body.Close()
	var response container.StatsResponse
	if err := json.NewDecoder(stats.Body).Decode(&response); err != nil {
		return nil, err
	}
	var rxBytes uint64
	var txBytes uint64
	for _, network := range response.Networks {
		rxBytes += network.RxBytes
		txBytes += network.TxBytes
	}
	traffic := globalTrafficStore.record(containerID, rxBytes, txBytes, time.Now())
	return &traffic, nil
}

func dockerPortBinding(bindings nat.PortMap, port domain.Port) (bool, string, int) {
	dockerPort := nat.Port(fmt.Sprintf("%d/%s", port.Port, normalizeProtocol(port.Protocol)))
	portBindings := bindings[dockerPort]
	if len(portBindings) == 0 {
		return false, "", 0
	}
	hostPort, _ := strconv.Atoi(portBindings[0].HostPort)
	return true, portBindings[0].HostIP, hostPort
}

func portsByName(ports []domain.Port) map[string]domain.Port {
	result := make(map[string]domain.Port, len(ports))
	for _, port := range ports {
		result[port.Name] = port
	}
	return result
}

func normalizeProtocol(protocol string) string {
	protocol = strings.ToLower(protocol)
	if protocol == "" || protocol == "http" || protocol == "https" {
		return "tcp"
	}
	return protocol
}
