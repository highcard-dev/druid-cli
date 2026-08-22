package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (b *Backend) ExpectedPorts(root string, commands map[string]*domain.CommandInstructionSet, ports []domain.Port) ([]domain.RuntimePortStatus, error) {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return nil, err
	}
	portsByName := portsByName(ports)
	statuses := []domain.RuntimePortStatus{}
	now := time.Now()
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
			traffic, trafficErr := b.procedureTrafficForSelector(context.Background(), namespace, procedureSelector(pvc, commandName, procedureName), now)
			for _, expectedPort := range procedure.ExpectedPorts {
				port := portsByName[expectedPort.Name]
				status := domain.RuntimePortStatus{
					Name:             expectedPort.Name,
					Procedure:        procedureName,
					Port:             port.Port,
					Protocol:         normalizeProtocol(port.Protocol),
					KeepAliveTraffic: expectedPort.KeepAliveTraffic,
					Source:           "kubernetes-service",
				}
				serviceReady, hostPort := b.serviceReady(context.Background(), namespace, portServiceName(root, expectedPort.Name))
				status.Bound = serviceReady
				status.HostPort = hostPort
				if trafficErr != nil {
					status.Source = "kubernetes-pod-stats-unavailable"
					statuses = append(statuses, status)
					continue
				}
				if traffic == nil {
					statuses = append(statuses, status)
					continue
				}
				status.Source = "kubernetes-pod-stats"
				rxBytes := traffic.rxBytes
				txBytes := traffic.txBytes
				status.RXBytes = &rxBytes
				status.TXBytes = &txBytes
				status.LastActivityAt = traffic.lastActivityAt
				delta := traffic.rxDelta(0, now)
				if expectedPort.KeepAliveTraffic != "" {
					threshold, err := domain.ParseKeepAliveTraffic(expectedPort.KeepAliveTraffic)
					if err != nil {
						return nil, err
					}
					delta = traffic.rxDelta(threshold.Window, now)
					trafficOK := delta >= threshold.Bytes
					status.TrafficOK = &trafficOK
					status.TrafficWindow = threshold.Window.String()
				}
				status.Traffic = delta > 0
				status.TrafficBytes = &delta
				statuses = append(statuses, status)
			}
		}
	}
	return statuses, nil
}

func (b *Backend) RoutingTargets(root string, commands map[string]*domain.CommandInstructionSet, ports []domain.Port) ([]domain.RuntimeRoutingTarget, error) {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return nil, err
	}
	portsByName := portsByName(ports)
	targets := []domain.RuntimeRoutingTarget{}
	seen := map[string]struct{}{}
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
			for _, expectedPort := range procedure.ExpectedPorts {
				if _, ok := seen[expectedPort.Name]; ok {
					continue
				}
				port := portsByName[expectedPort.Name]
				seen[expectedPort.Name] = struct{}{}
				targets = append(targets, domain.RuntimeRoutingTarget{
					Name:        expectedPort.Name,
					Procedure:   procedureName,
					PortName:    expectedPort.Name,
					Port:        port.Port,
					Protocol:    normalizeProtocol(port.Protocol),
					Namespace:   namespace,
					ServiceName: portServiceName(root, expectedPort.Name),
					Selector:    portServiceSelector(pvc, expectedPort.Name),
				})
			}
		}
	}
	for _, port := range ports {
		if _, ok := seen[port.Name]; ok {
			continue
		}
		targets = append(targets, domain.RuntimeRoutingTarget{
			Name:        port.Name,
			PortName:    port.Name,
			Port:        port.Port,
			Protocol:    normalizeProtocol(port.Protocol),
			Namespace:   namespace,
			ServiceName: portServiceName(root, port.Name),
			Selector:    portServiceSelector(pvc, port.Name),
		})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	return targets, nil
}

func (b *Backend) ensureExpectedServices(ctx context.Context, root string, commandName string, procedureName string, procedure *domain.Procedure, ports []domain.Port) error {
	namespace, _, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot reconcile Kubernetes Services for invalid root", zap.String("root", root), zap.String("command", commandName), zap.String("procedure", procedureName), zap.Error(err))
		return err
	}
	portsByName := portsByName(ports)
	for _, expected := range procedure.ExpectedPorts {
		port := portsByName[expected.Name]
		service, err := serviceSpec(namespace, root, portServiceSelector(refPVCName(root), expected.Name), expected.Name, port)
		if err != nil {
			logger.Log().Error("Failed to build Kubernetes Service for expected port", zap.String("namespace", namespace), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("port", expected.Name), zap.Error(err))
			return err
		}
		logger.Log().Debug("Reconciling Kubernetes expected-port Service",
			zap.String("namespace", namespace),
			zap.String("command", commandName),
			zap.String("procedure", procedureName),
			zap.String("service", service.Name),
			zap.String("port_name", expected.Name),
			zap.Int("port", port.Port),
			zap.String("protocol", port.Protocol),
			zap.Any("selector", service.Spec.Selector),
		)
		current, err := b.client.CoreV1().Services(namespace).Get(ctx, service.Name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			logger.Log().Info("Creating Kubernetes expected-port Service", zap.String("namespace", namespace), zap.String("service", service.Name), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("port_name", expected.Name))
			if _, err := b.client.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{}); err != nil {
				logger.Log().Error("Failed to create Kubernetes expected-port Service", zap.String("namespace", namespace), zap.String("service", service.Name), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("port_name", expected.Name), zap.Error(err))
				return err
			}
		case err != nil:
			logger.Log().Error("Failed to get Kubernetes expected-port Service", zap.String("namespace", namespace), zap.String("service", service.Name), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("port_name", expected.Name), zap.Error(err))
			return err
		default:
			logger.Log().Info("Updating Kubernetes expected-port Service", zap.String("namespace", namespace), zap.String("service", service.Name), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("port_name", expected.Name), zap.String("resource_version", current.ResourceVersion))
			service.ResourceVersion = current.ResourceVersion
			service.Spec.ClusterIP = current.Spec.ClusterIP
			service.Spec.ClusterIPs = current.Spec.ClusterIPs
			service.Spec.IPFamilies = current.Spec.IPFamilies
			service.Spec.IPFamilyPolicy = current.Spec.IPFamilyPolicy
			if _, err := b.client.CoreV1().Services(namespace).Update(ctx, service, metav1.UpdateOptions{}); err != nil {
				logger.Log().Error("Failed to update Kubernetes expected-port Service", zap.String("namespace", namespace), zap.String("service", service.Name), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("port_name", expected.Name), zap.Error(err))
				return err
			}
		}
		if err := b.deleteLegacyPortServices(ctx, namespace, root, expected.Name, service.Name); err != nil {
			return err
		}
	}
	return nil
}

func (b *Backend) deleteLegacyPortServices(ctx context.Context, namespace string, root string, portName string, stableName string) error {
	_, pvc, err := parseRef(root)
	if err != nil {
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID: dnsLabel(pvc),
		labelPortName: dnsLabel(portName),
	}).String()
	services, err := b.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	for _, service := range services.Items {
		if service.Name == stableName {
			continue
		}
		if err := b.client.CoreV1().Services(namespace).Delete(ctx, service.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func portServiceName(root string, portName string) string {
	return serviceName(root, portName, portName)
}

func procedureSelector(pvc string, commandName string, procedureName string) map[string]string {
	selector := baseLabels(pvc)
	selector[labelCommand] = dnsLabel(commandName)
	selector[labelProcedure] = dnsLabel(procedureName)
	return selector
}

func portServiceSelector(pvc string, portName string) map[string]string {
	selector := baseLabels(pvc)
	selector[portSelectorLabel(portName)] = "true"
	return selector
}

func (b *Backend) serviceReady(ctx context.Context, namespace string, name string) (bool, int) {
	service, err := b.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, 0
	}
	hostPort := 0
	if len(service.Spec.Ports) > 0 {
		hostPort = int(service.Spec.Ports[0].Port)
	}
	selector := labels.SelectorFromSet(labels.Set{"kubernetes.io/service-name": name}).String()
	slices, err := b.client.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return false, hostPort
	}
	return endpointSlicesReady(slices.Items), hostPort
}

func endpointSlicesReady(slices []discoveryv1.EndpointSlice) bool {
	for _, slice := range slices {
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready {
				return true
			}
		}
	}
	return false
}

func portsByName(ports []domain.Port) map[string]domain.Port {
	result := map[string]domain.Port{}
	for _, port := range ports {
		result[port.Name] = port
	}
	return result
}

func normalizeProtocol(protocol string) string {
	switch strings.ToLower(protocol) {
	case "", "tcp":
		return "tcp"
	case "udp":
		return "udp"
	default:
		return protocol
	}
}
