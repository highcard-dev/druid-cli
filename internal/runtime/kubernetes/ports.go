package kubernetes

import (
	"context"
	"errors"
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

func (b *Backend) ExpectedPorts(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimePortStatus, error) {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return nil, err
	}
	portsByName := portsByName(globalPorts)
	statuses := []domain.RuntimePortStatus{}
	hubbleAvailable := true
	if err := b.checkHubble(context.Background()); err != nil {
		hubbleAvailable = false
		logger.Log().Warn("Hubble Relay unavailable; Kubernetes port traffic unavailable", zap.Error(err))
	}
	for commandName, command := range commands {
		if command == nil {
			continue
		}
		portUse := expectedPortUse(command)
		for idx, procedure := range command.Procedures {
			if procedure == nil || len(procedure.ExpectedPorts) == 0 {
				continue
			}
			procedureName := fmt.Sprintf("%s.%d", commandName, idx)
			if procedure.Id != nil {
				procedureName = *procedure.Id
			}
			for _, expectedPort := range procedure.ExpectedPorts {
				port, ok := portsByName[expectedPort.Name]
				if !ok {
					return nil, fmt.Errorf("expected port %s is not defined in top-level ports", expectedPort.Name)
				}
				status := domain.RuntimePortStatus{
					Name:             expectedPort.Name,
					Procedure:        procedureName,
					Port:             port.Port,
					Protocol:         normalizeProtocol(port.Protocol),
					KeepAliveTraffic: expectedPort.KeepAliveTraffic,
					Source:           "kubernetes-service",
				}
				serviceProcedure := serviceProcedureName(commandName, procedureName, expectedPort.Name, portUse)
				serviceReady, hostPort := b.serviceReady(context.Background(), namespace, serviceName(root, serviceProcedure, expectedPort.Name))
				status.Bound = serviceReady
				status.HostPort = hostPort
				if !hubbleAvailable {
					status.Source = "hubble-relay-unavailable"
					statuses = append(statuses, status)
					continue
				}
				window := 5 * time.Minute
				if expectedPort.KeepAliveTraffic != "" {
					threshold, err := domain.ParseKeepAliveTraffic(expectedPort.KeepAliveTraffic)
					if err != nil {
						return nil, err
					}
					window = threshold.Window
					status.TrafficWindow = threshold.Window.String()
				}
				traffic, err := b.hubble.HasFlow(context.Background(), TrafficQuery{
					Namespace:     namespace,
					ScrollID:      pvc,
					ProcedureName: procedureName,
					Port:          port,
					ExpectedPort:  expectedPort,
					Window:        window,
				})
				if err != nil {
					logger.Log().Warn("Hubble Relay query failed", zap.Error(err))
					status.Source = "hubble-relay-unavailable"
					statuses = append(statuses, status)
					continue
				}
				status.Source = "hubble-relay"
				status.Traffic = traffic
				if expectedPort.KeepAliveTraffic != "" {
					trafficOK := traffic
					status.TrafficOK = &trafficOK
				}
				statuses = append(statuses, status)
			}
		}
	}
	return statuses, nil
}

func (b *Backend) RoutingTargets(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimeRoutingTarget, error) {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return nil, err
	}
	portsByName := portsByName(globalPorts)
	targets := []domain.RuntimeRoutingTarget{{
		Name:        "webdav",
		Procedure:   "dev",
		PortName:    "webdav",
		Port:        8084,
		Protocol:    "https",
		Namespace:   namespace,
		ServiceName: serviceName(root, "dev", "webdav"),
		ServicePort: 8084,
		Selector: map[string]string{
			labelManagedBy: "druid",
			labelComponent: "runtime",
			labelScrollID:  dnsLabel(pvc),
			labelProcedure: "dev",
		},
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
		portUse := expectedPortUse(command)
		for idx, procedure := range command.Procedures {
			if procedure == nil || len(procedure.ExpectedPorts) == 0 {
				continue
			}
			procedureName := domain.ProcedureName(commandName, idx, procedure)
			for _, expectedPort := range procedure.ExpectedPorts {
				if _, ok := seen[expectedPort.Name]; ok {
					continue
				}
				port, ok := portsByName[expectedPort.Name]
				if !ok {
					return nil, fmt.Errorf("expected port %s is not defined in top-level ports", expectedPort.Name)
				}
				seen[expectedPort.Name] = struct{}{}
				serviceProcedure := serviceProcedureName(commandName, procedureName, expectedPort.Name, portUse)
				svcName := serviceName(root, serviceProcedure, expectedPort.Name)
				selector := serviceSelector(pvc, commandName, procedureName, expectedPort.Name, portUse)
				if currentSelector, ok := b.currentServiceSelector(context.Background(), namespace, svcName); ok {
					selector = currentSelector
					if currentProcedure := currentSelector[labelProcedure]; currentProcedure != "" {
						procedureName = currentProcedure
					}
				}
				targets = append(targets, domain.RuntimeRoutingTarget{
					Name:        expectedPort.Name,
					Procedure:   procedureName,
					PortName:    expectedPort.Name,
					Port:        port.Port,
					Protocol:    normalizeProtocol(port.Protocol),
					Namespace:   namespace,
					ServiceName: svcName,
					ServicePort: port.Port,
					Selector:    selector,
				})
			}
		}
	}
	return targets, nil
}

func (b *Backend) ensureExpectedServices(ctx context.Context, root string, commandName string, procedureName string, procedure *domain.Procedure, globalPorts []domain.Port, portUse map[string]int) error {
	namespace, _, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot reconcile Kubernetes Services for invalid root", zap.String("root", root), zap.String("command", commandName), zap.String("procedure", procedureName), zap.Error(err))
		return err
	}
	ports := portsByName(globalPorts)
	for _, expected := range procedure.ExpectedPorts {
		port, ok := ports[expected.Name]
		if !ok {
			err := fmt.Errorf("expected port %s is not defined in top-level ports", expected.Name)
			logger.Log().Error("Kubernetes expected port has no top-level port definition", zap.String("namespace", namespace), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("port", expected.Name), zap.Error(err))
			return err
		}
		serviceProcedure := serviceProcedureName(commandName, procedureName, expected.Name, portUse)
		service, err := serviceSpec(namespace, root, serviceProcedure, serviceSelector(refPVCName(root), commandName, procedureName, expected.Name, portUse), expected.Name, port)
		if err != nil {
			logger.Log().Error("Failed to build Kubernetes Service for expected port", zap.String("namespace", namespace), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("port", expected.Name), zap.Error(err))
			return err
		}
		logger.Log().Debug("Reconciling Kubernetes expected-port Service",
			zap.String("namespace", namespace),
			zap.String("command", commandName),
			zap.String("procedure", procedureName),
			zap.String("service_procedure", serviceProcedure),
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
	}
	return nil
}

func expectedPortUse(command *domain.CommandInstructionSet) map[string]int {
	use := map[string]int{}
	if command == nil {
		return use
	}
	for _, procedure := range command.Procedures {
		if procedure == nil {
			continue
		}
		for _, expected := range procedure.ExpectedPorts {
			use[expected.Name]++
		}
	}
	return use
}

func serviceProcedureName(commandName string, procedureName string, portName string, portUse map[string]int) string {
	if portUse[portName] > 1 {
		return commandName
	}
	return procedureName
}

func serviceSelector(pvc string, commandName string, procedureName string, portName string, portUse map[string]int) map[string]string {
	selector := baseLabels(pvc)
	selector[labelCommand] = dnsLabel(commandName)
	selector[labelProcedure] = dnsLabel(procedureName)
	return selector
}

func (b *Backend) currentServiceSelector(ctx context.Context, namespace string, name string) (map[string]string, bool) {
	service, err := b.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || len(service.Spec.Selector) == 0 {
		return nil, false
	}
	selector := make(map[string]string, len(service.Spec.Selector))
	for key, value := range service.Spec.Selector {
		selector[key] = value
	}
	return selector, true
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

func (b *Backend) checkHubble(ctx context.Context) error {
	if b.hubble == nil {
		return errors.New("hubble client is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := b.hubble.HasFlow(ctx, TrafficQuery{Namespace: b.config.Namespace, Port: domain.Port{Port: 1, Protocol: "tcp"}})
	if err != nil && !strings.Contains(err.Error(), "context deadline") {
		return err
	}
	return nil
}

func portsByName(ports []domain.Port) map[string]domain.Port {
	result := map[string]domain.Port{}
	for _, port := range ports {
		result[port.Name] = port
	}
	return result
}
