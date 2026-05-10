package kubernetes

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type Backend struct {
	client         k8sclient.Interface
	consoleManager ports.ConsoleManagerInterface
	config         Config
	hubble         HubbleClient
}

func New(config Config, consoleManager ports.ConsoleManagerInterface) (*Backend, error) {
	config = config.WithDefaults()

	restConfig, namespace, source, inCluster, err := runtimeRESTConfig(config)
	if err != nil {
		return nil, err
	}
	config.Namespace = namespace
	if err := config.ValidateForBackend(); err != nil {
		return nil, err
	}

	client, err := k8sclient.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	if _, err := client.Discovery().ServerVersion(); err != nil {
		return nil, fmt.Errorf("kubernetes API unavailable: %w", err)
	}
	logger.Log().Info("Using Kubernetes runtime config", zap.String("source", source), zap.String("namespace", config.Namespace))
	backend := &Backend{
		client:         client,
		consoleManager: consoleManager,
		config:         config,
		hubble:         NewHubbleRelayClient(config.HubbleRelayAddr),
	}
	if config.PullImage == "" {
		logger.Log().Warn("Kubernetes cluster materialization requires --k8s-pull-image or DRUID_K8S_PULL_IMAGE")
	}
	if !inCluster && config.HubbleRelayAddr == defaultHubbleRelayAddr {
		logger.Log().Warn("Default Hubble Relay address may not be reachable outside the cluster; set --hubble-relay-addr or port-forward Hubble Relay", zap.String("addr", config.HubbleRelayAddr))
	}
	if err := backend.checkHubble(context.Background()); err != nil {
		logger.Log().Warn("Hubble Relay unavailable; Kubernetes port traffic will degrade to Service/Endpoint status", zap.Error(err), zap.String("addr", config.HubbleRelayAddr))
	}
	return backend, nil
}

func runtimeRESTConfig(config Config) (*rest.Config, string, string, bool, error) {
	restConfig, inClusterErr := rest.InClusterConfig()
	if inClusterErr == nil {
		namespace := config.Namespace
		if namespace == "" {
			namespace = namespaceFromServiceAccount()
		}
		if namespace == "" {
			namespace = "default"
		}
		return restConfig, namespace, "in-cluster", true, nil
	}

	restConfig, namespace, source, kubeconfigErr := kubeconfigRESTConfig(config)
	if kubeconfigErr != nil {
		return nil, "", "", false, fmt.Errorf("kubernetes runtime could not load auth: in-cluster config unavailable (%v); kubeconfig unavailable (%w)", inClusterErr, kubeconfigErr)
	}
	return restConfig, namespace, source, false, nil
}

func kubeconfigRESTConfig(config Config) (*rest.Config, string, string, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	source := "kubeconfig"
	if config.Kubeconfig != "" {
		loadingRules.ExplicitPath = config.Kubeconfig
		source = config.Kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if config.Namespace != "" {
		overrides.Context.Namespace = config.Namespace
	}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, "", source, err
	}
	namespace, _, err := clientConfig.Namespace()
	if err != nil || namespace == "" {
		namespace = "default"
	}
	return restConfig, namespace, source, nil
}

func NewWithClient(config Config, consoleManager ports.ConsoleManagerInterface, client k8sclient.Interface, hubble HubbleClient) *Backend {
	config = config.WithDefaults()
	if config.Namespace == "" {
		config.Namespace = "default"
	}
	if hubble == nil {
		hubble = NewHubbleRelayClient(config.HubbleRelayAddr)
	}
	return &Backend{client: client, consoleManager: consoleManager, config: config, hubble: hubble}
}

func (b *Backend) Name() string {
	return "kubernetes"
}

func (b *Backend) MaterializeScroll(ctx context.Context, artifact string, requestedName string) (*ports.RuntimeMaterialization, error) {
	if err := b.config.ValidateForMaterialization(); err != nil {
		return nil, err
	}
	stagePVC := stagingPVCName(artifact + requestedName)
	if err := b.ensurePVC(ctx, stagePVC); err != nil {
		return nil, err
	}
	pullJob := pullJobSpec(b.config.Namespace, jobName("pull", ref(b.config.Namespace, stagePVC), shortHash(artifact)), stagePVC, b.config.PullImage, artifact, b.config.RegistrySecret, b.config.RegistryPlainHTTP)
	if err := b.runHelperJob(ctx, pullJob); err != nil {
		return nil, err
	}
	scrollYAML, err := b.ReadScrollFile(ref(b.config.Namespace, stagePVC))
	if err != nil {
		return nil, err
	}
	scroll, err := domain.NewScrollFromBytes("", scrollYAML)
	if err != nil {
		return nil, err
	}
	id, err := coreservices.RuntimeScrollID(requestedName, scroll.Name)
	if err != nil {
		return nil, err
	}
	finalPVC := dataPVCName(id)
	if err := b.ensurePVC(ctx, finalPVC); err != nil {
		return nil, err
	}
	copyJob := copyPVCJobSpec(b.config.Namespace, jobName("copy", ref(b.config.Namespace, finalPVC), shortHash(stagePVC)), stagePVC, finalPVC, b.config.HelperImage)
	if err := b.runHelperJob(ctx, copyJob); err != nil {
		return nil, err
	}
	_ = b.client.CoreV1().PersistentVolumeClaims(b.config.Namespace).Delete(ctx, stagePVC, metav1.DeleteOptions{})
	return &ports.RuntimeMaterialization{
		Artifact:   artifact,
		ScrollRoot: ref(b.config.Namespace, finalPVC),
		DataRoot:   ref(b.config.Namespace, finalPVC),
		ScrollYAML: scrollYAML,
	}, nil
}

func (b *Backend) ReadScrollFile(scrollRoot string) ([]byte, error) {
	namespace, pvc, err := parseRef(scrollRoot)
	if err != nil {
		return nil, err
	}
	job := readScrollJobSpec(namespace, jobName("read", scrollRoot, "scroll-yaml"), pvc, b.config.HelperImage)
	return b.runJobAndLogs(context.Background(), job)
}

func (b *Backend) ReadDataFile(ctx context.Context, dataRoot string, relativePath string) ([]byte, error) {
	cleaned, err := cleanDataPath(relativePath)
	if err != nil {
		return nil, err
	}
	namespace, pvc, err := parseRef(dataRoot)
	if err != nil {
		return nil, err
	}
	job := readDataFileJobSpec(namespace, jobName("read-file", dataRoot, shortHash(cleaned)), pvc, b.config.HelperImage, cleaned)
	return b.runJobAndLogs(ctx, job)
}

func (b *Backend) WriteDataFile(ctx context.Context, dataRoot string, relativePath string, data []byte) error {
	cleaned, err := cleanDataPath(relativePath)
	if err != nil {
		return err
	}
	namespace, pvc, err := parseRef(dataRoot)
	if err != nil {
		return err
	}
	job := writeDataFileJobSpec(namespace, jobName("write-file", dataRoot, shortHash(cleaned)), pvc, b.config.HelperImage, cleaned, base64.StdEncoding.EncodeToString(data))
	return b.runHelperJob(ctx, job)
}

func cleanDataPath(relativePath string) (string, error) {
	cleaned := path.Clean(strings.TrimPrefix(relativePath, "/"))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid data file path %q", relativePath)
	}
	return cleaned, nil
}

func (b *Backend) RunCommand(command ports.RuntimeCommand) (*int, error) {
	for idx, procedure := range command.Command.Procedures {
		procedureName := domain.ProcedureName(command.Name, idx, procedure)
		env := command.ProcedureEnv[procedureName]
		if env == nil {
			env = procedure.Env
		}
		if command.Command.Run == domain.RunModePersistent {
			if procedure.IsSignal() {
				if err := b.Signal(procedureName, procedure.Target, procedure.Signal, command.DataRoot); err != nil {
					return nil, err
				}
				continue
			}
			if procedure.Image == "" {
				return nil, fmt.Errorf("kubernetes procedure %s requires image", procedureName)
			}
			if err := b.ensurePersistentProcedure(context.Background(), command.ScrollID, command.DataRoot, procedureName, procedure, command.GlobalPorts, env); err != nil {
				return nil, err
			}
			continue
		}
		exitCode, err := b.runJobProcedure(command.ScrollID, procedureName, procedure, command.DataRoot, command.GlobalPorts, env)
		if err != nil {
			return exitCode, err
		}
		if exitCode != nil && *exitCode != 0 {
			if procedure.IgnoreFailure {
				continue
			}
			return exitCode, nil
		}
	}
	return nil, nil
}

func (b *Backend) runJobProcedure(scrollID string, procedureName string, procedure *domain.Procedure, dataRoot string, globalPorts []domain.Port, env map[string]string) (*int, error) {
	if procedure.IsSignal() {
		return nil, b.Signal(procedureName, procedure.Target, procedure.Signal, dataRoot)
	}
	if procedure.Image == "" {
		return nil, fmt.Errorf("kubernetes procedure %s requires image", procedureName)
	}
	ctx := context.Background()
	if err := b.ensureExpectedServices(ctx, dataRoot, procedureName, procedure, globalPorts); err != nil {
		return nil, err
	}
	job, err := procedureJobSpec(b.config.Namespace, dataRoot, procedureName, procedure, env, b.config.RegistrySecret)
	if err != nil {
		return nil, err
	}
	createdJob, err := b.createFreshJob(ctx, job)
	if err != nil {
		return nil, err
	}
	output := make(chan string, 100)
	consoleID := runtimeConsoleID(scrollID, procedureName)
	console, doneChan := b.consoleManager.AddConsoleWithChannel(consoleID, domain.ConsoleTypeContainer, "stdin", output)
	console.WriteInput = func(data string) error {
		return b.Attach(procedureName, data)
	}
	streamStarted := false
	podName, err := b.waitForJobPod(ctx, job.Name, string(createdJob.UID))
	if err == nil {
		streamStarted = true
		go b.streamPodLogs(ctx, podName, output)
	}
	exitCode, err := b.waitForJob(ctx, job.Name)
	if exitCode != nil {
		console.MarkExited(*exitCode)
	}
	if !streamStarted {
		close(output)
	}
	<-doneChan
	if err != nil {
		return exitCode, err
	}
	return exitCode, nil
}

func (b *Backend) ensurePersistentProcedure(ctx context.Context, scrollID string, dataRoot string, procedureName string, procedure *domain.Procedure, globalPorts []domain.Port, env map[string]string) error {
	if err := b.ensureExpectedServices(ctx, dataRoot, procedureName, procedure, globalPorts); err != nil {
		return err
	}
	statefulSet, err := procedureStatefulSetSpec(b.config.Namespace, dataRoot, procedureName, procedure, env, b.config.RegistrySecret)
	if err != nil {
		return err
	}
	existing, err := b.client.AppsV1().StatefulSets(b.config.Namespace).Get(ctx, statefulSet.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		if _, err := b.client.AppsV1().StatefulSets(b.config.Namespace).Create(ctx, statefulSet, metav1.CreateOptions{}); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		statefulSet.ResourceVersion = existing.ResourceVersion
		if _, err := b.client.AppsV1().StatefulSets(b.config.Namespace).Update(ctx, statefulSet, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	output := make(chan string, 100)
	console, _ := b.consoleManager.AddConsoleWithChannel(runtimeConsoleID(scrollID, procedureName), domain.ConsoleTypeContainer, "stdin", output)
	console.WriteInput = func(data string) error {
		return b.Attach(procedureName, data)
	}
	if err := b.waitForStatefulSet(ctx, statefulSet.Name); err != nil {
		close(output)
		return err
	}
	go func() {
		podName, err := b.waitForPodBySelector(context.Background(), labels.SelectorFromSet(labels.Set{
			labelScrollID:  statefulSet.Labels[labelScrollID],
			labelProcedure: statefulSet.Labels[labelProcedure],
		}).String())
		if err != nil {
			output <- fmt.Sprintf("failed to find StatefulSet pod logs: %v", err)
			close(output)
			return
		}
		b.streamPodLogs(context.Background(), podName, output)
	}()
	return nil
}

func (b *Backend) ExpectedPorts(dataRoot string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimePortStatus, error) {
	_, pvc, err := parseRef(dataRoot)
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
				serviceReady, hostPort := b.serviceReady(context.Background(), serviceName(dataRoot, procedureName, expectedPort.Name))
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
					Namespace:     b.config.Namespace,
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

func (b *Backend) RoutingTargets(dataRoot string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimeRoutingTarget, error) {
	namespace, pvc, err := parseRef(dataRoot)
	if err != nil {
		return nil, err
	}
	portsByName := portsByName(globalPorts)
	targets := []domain.RuntimeRoutingTarget{}
	for commandName, command := range commands {
		if command == nil {
			continue
		}
		for idx, procedure := range command.Procedures {
			if procedure == nil || len(procedure.ExpectedPorts) == 0 {
				continue
			}
			procedureName := domain.ProcedureName(commandName, idx, procedure)
			for _, expectedPort := range procedure.ExpectedPorts {
				port, ok := portsByName[expectedPort.Name]
				if !ok {
					return nil, fmt.Errorf("expected port %s is not defined in top-level ports", expectedPort.Name)
				}
				targets = append(targets, domain.RuntimeRoutingTarget{
					Name:        fmt.Sprintf("%s-%s", procedureName, expectedPort.Name),
					Procedure:   procedureName,
					PortName:    expectedPort.Name,
					Port:        port.Port,
					Protocol:    normalizeProtocol(port.Protocol),
					Namespace:   namespace,
					ServiceName: serviceName(dataRoot, procedureName, expectedPort.Name),
					ServicePort: port.Port,
					Selector: map[string]string{
						labelManagedBy: "druid",
						labelComponent: "runtime",
						labelScrollID:  dnsLabel(pvc),
						labelProcedure: dnsLabel(procedureName),
					},
				})
			}
		}
	}
	return targets, nil
}

func (b *Backend) StopRuntime(dataRoot string) error {
	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{PropagationPolicy: &propagation}
	if err := b.deleteRuntimeJobs(context.Background(), dataRoot, options); err != nil {
		return err
	}
	if err := b.deleteRuntimeStatefulSets(context.Background(), dataRoot, options); err != nil {
		return err
	}
	return b.deleteRuntimePodsByScroll(context.Background(), dataRoot, options)
}

func (b *Backend) DeleteRuntime(dataRoot string, purgeData bool) error {
	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{PropagationPolicy: &propagation}
	if err := b.StopRuntime(dataRoot); err != nil {
		return err
	}
	if err := b.deleteRuntimeServices(context.Background(), dataRoot, options); err != nil {
		return err
	}
	if purgeData {
		namespace, pvc, err := parseRef(dataRoot)
		if err != nil {
			return err
		}
		err = b.client.CoreV1().PersistentVolumeClaims(namespace).Delete(context.Background(), pvc, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (b *Backend) BackupRuntime(ctx context.Context, dataRoot string, artifact string) error {
	if artifact == "" {
		return fmt.Errorf("backup artifact is required")
	}
	if b.config.PullImage == "" {
		return b.config.ValidateForMaterialization()
	}
	namespace, pvc, err := parseRef(dataRoot)
	if err != nil {
		return err
	}
	job := backupJobSpec(namespace, jobName("backup", dataRoot, shortHash(artifact)), pvc, b.config.PullImage, artifact, b.config.RegistrySecret, b.config.RegistryPlainHTTP)
	return b.runHelperJob(ctx, job)
}

func (b *Backend) RestoreRuntime(ctx context.Context, dataRoot string, artifact string) error {
	if artifact == "" {
		return fmt.Errorf("restore artifact is required")
	}
	if err := b.config.ValidateForMaterialization(); err != nil {
		return err
	}
	namespace, pvc, err := parseRef(dataRoot)
	if err != nil {
		return err
	}
	stagePVC := stagingPVCName("restore:" + dataRoot + ":" + artifact)
	if err := b.ensurePVC(ctx, stagePVC); err != nil {
		return err
	}
	defer b.client.CoreV1().PersistentVolumeClaims(namespace).Delete(context.Background(), stagePVC, metav1.DeleteOptions{})
	pullJob := pullJobSpec(namespace, jobName("restore-pull", ref(namespace, stagePVC), shortHash(artifact)), stagePVC, b.config.PullImage, artifact, b.config.RegistrySecret, b.config.RegistryPlainHTTP)
	if err := b.runHelperJob(ctx, pullJob); err != nil {
		return err
	}
	if err := b.StopRuntime(dataRoot); err != nil {
		return err
	}
	restoreJob := replacePVCJobSpec(namespace, jobName("restore-copy", dataRoot, shortHash(artifact)), stagePVC, pvc, b.config.HelperImage)
	return b.runHelperJob(ctx, restoreJob)
}

func (b *Backend) Attach(commandName string, data string) error {
	return fmt.Errorf("kubernetes attach is not implemented for console %s: pod attach/exec support is required", commandName)
}

func (b *Backend) Signal(_ string, target string, signal string, dataRoot string) error {
	if target == "" {
		return nil
	}
	switch signal {
	case "", "SIGTERM", "TERM":
		propagation := metav1.DeletePropagationBackground
		return b.deleteRuntimeWorkload(context.Background(), dataRoot, target, metav1.DeleteOptions{PropagationPolicy: &propagation})
	case "SIGKILL", "KILL":
		grace := int64(0)
		propagation := metav1.DeletePropagationBackground
		return b.deleteRuntimeWorkload(context.Background(), dataRoot, target, metav1.DeleteOptions{GracePeriodSeconds: &grace, PropagationPolicy: &propagation})
	default:
		return fmt.Errorf("kubernetes signal %s is unsupported without pod exec", signal)
	}
}

func (b *Backend) deleteRuntimeWorkload(ctx context.Context, dataRoot string, target string, options metav1.DeleteOptions) error {
	jobErr := b.client.BatchV1().Jobs(b.config.Namespace).Delete(ctx, jobName("proc", dataRoot, target), options)
	if apierrors.IsNotFound(jobErr) {
		jobErr = nil
	}
	statefulSetErr := b.client.AppsV1().StatefulSets(b.config.Namespace).Delete(ctx, statefulSetName(dataRoot, target), options)
	if apierrors.IsNotFound(statefulSetErr) {
		statefulSetErr = nil
	}
	podErr := b.deleteRuntimePods(ctx, dataRoot, target, options)
	if jobErr != nil {
		return jobErr
	}
	if statefulSetErr != nil {
		return statefulSetErr
	}
	return podErr
}

func (b *Backend) deleteRuntimePods(ctx context.Context, dataRoot string, target string, options metav1.DeleteOptions) error {
	_, pvc, err := parseRef(dataRoot)
	if err != nil {
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID:  dnsLabel(pvc),
		labelProcedure: dnsLabel(target),
	}).String()
	pods, err := b.client.CoreV1().Pods(b.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		if err := b.client.CoreV1().Pods(b.config.Namespace).Delete(ctx, pod.Name, options); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (b *Backend) deleteRuntimeJobs(ctx context.Context, dataRoot string, options metav1.DeleteOptions) error {
	return b.deleteRuntimeObjects(ctx, dataRoot, func(name string) error {
		err := b.client.BatchV1().Jobs(b.config.Namespace).Delete(ctx, name, options)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}, "jobs")
}

func (b *Backend) deleteRuntimeStatefulSets(ctx context.Context, dataRoot string, options metav1.DeleteOptions) error {
	return b.deleteRuntimeObjects(ctx, dataRoot, func(name string) error {
		err := b.client.AppsV1().StatefulSets(b.config.Namespace).Delete(ctx, name, options)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}, "statefulsets")
}

func (b *Backend) deleteRuntimeServices(ctx context.Context, dataRoot string, options metav1.DeleteOptions) error {
	return b.deleteRuntimeObjects(ctx, dataRoot, func(name string) error {
		err := b.client.CoreV1().Services(b.config.Namespace).Delete(ctx, name, options)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}, "services")
}

func (b *Backend) deleteRuntimeObjects(ctx context.Context, dataRoot string, deleteOne func(name string) error, kind string) error {
	_, pvc, err := parseRef(dataRoot)
	if err != nil {
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID: dnsLabel(pvc),
	}).String()
	switch kind {
	case "jobs":
		items, err := b.client.BatchV1().Jobs(b.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}
		for _, item := range items.Items {
			if err := deleteOne(item.Name); err != nil {
				return err
			}
		}
	case "statefulsets":
		items, err := b.client.AppsV1().StatefulSets(b.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}
		for _, item := range items.Items {
			if err := deleteOne(item.Name); err != nil {
				return err
			}
		}
	case "services":
		items, err := b.client.CoreV1().Services(b.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}
		for _, item := range items.Items {
			if err := deleteOne(item.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Backend) deleteRuntimePodsByScroll(ctx context.Context, dataRoot string, options metav1.DeleteOptions) error {
	_, pvc, err := parseRef(dataRoot)
	if err != nil {
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID: dnsLabel(pvc),
	}).String()
	pods, err := b.client.CoreV1().Pods(b.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		if err := b.client.CoreV1().Pods(b.config.Namespace).Delete(ctx, pod.Name, options); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (b *Backend) ensurePVC(ctx context.Context, name string) error {
	pvc := pvcSpec(b.config.Namespace, name, b.config.StorageClass)
	_, err := b.client.CoreV1().PersistentVolumeClaims(b.config.Namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (b *Backend) runHelperJob(ctx context.Context, job *batchv1.Job) error {
	_, err := b.runJobAndLogs(ctx, job)
	return err
}

func (b *Backend) runJobAndLogs(ctx context.Context, job *batchv1.Job) ([]byte, error) {
	createdJob, err := b.createFreshJob(ctx, job)
	if err != nil {
		return nil, err
	}
	podName, err := b.waitForJobPod(ctx, job.Name, string(createdJob.UID))
	if err != nil {
		return nil, err
	}
	exitCode, waitErr := b.waitForJob(ctx, job.Name)
	logs, logErr := b.podLogs(ctx, podName)
	if logErr != nil && waitErr == nil {
		waitErr = logErr
	}
	if waitErr != nil {
		return logs, waitErr
	}
	if exitCode != nil && *exitCode != 0 {
		return logs, fmt.Errorf("job %s exited with code %d", job.Name, *exitCode)
	}
	return logs, nil
}

func (b *Backend) createFreshJob(ctx context.Context, job *batchv1.Job) (*batchv1.Job, error) {
	propagation := metav1.DeletePropagationBackground
	deleteCtx, cancelDelete := context.WithTimeout(ctx, 30*time.Second)
	defer cancelDelete()
	if err := b.client.BatchV1().Jobs(job.Namespace).Delete(deleteCtx, job.Name, metav1.DeleteOptions{PropagationPolicy: &propagation}); err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	for {
		_, err := b.client.BatchV1().Jobs(job.Namespace).Get(deleteCtx, job.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			break
		}
		if err != nil {
			return nil, err
		}
		select {
		case <-deleteCtx.Done():
			return nil, deleteCtx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return b.client.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
}

func (b *Backend) waitForJobPod(ctx context.Context, jobName string, controllerUID string) (string, error) {
	matchLabels := labels.Set{"job-name": jobName}
	if controllerUID != "" {
		matchLabels["controller-uid"] = controllerUID
	}
	selector := labels.SelectorFromSet(matchLabels).String()
	return b.waitForPodBySelector(ctx, selector)
}

func (b *Backend) waitForPodBySelector(ctx context.Context, selector string) (string, error) {
	deadline := time.Now().Add(2 * time.Minute)
	backoff := newCappedBackoff(podPollInitial, podPollMax)
	for {
		pods, err := b.client.CoreV1().Pods(b.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return "", err
		}
		if len(pods.Items) > 0 {
			return pods.Items[0].Name, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for pod matching selector %s", selector)
		}
		if err := sleepUntilNextPoll(ctx, deadline, backoff.Next()); err != nil {
			return "", err
		}
	}
}

func (b *Backend) waitForStatefulSet(ctx context.Context, name string) error {
	deadline := time.Now().Add(5 * time.Minute)
	backoff := newCappedBackoff(statefulSetPollInitial, statefulSetPollMax)
	for {
		statefulSet, err := b.client.AppsV1().StatefulSets(b.config.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		wanted := int32(1)
		if statefulSet.Spec.Replicas != nil {
			wanted = *statefulSet.Spec.Replicas
		}
		if statefulSet.Status.ReadyReplicas >= wanted {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for StatefulSet %s to become ready", name)
		}
		if err := sleepUntilNextPoll(ctx, deadline, backoff.Next()); err != nil {
			return err
		}
	}
}

func (b *Backend) waitForJob(ctx context.Context, jobName string) (*int, error) {
	startedAt := time.Now()
	deadline := time.Now().Add(24 * time.Hour)
	for {
		job, err := b.client.BatchV1().Jobs(b.config.Namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if job.Status.Succeeded > 0 {
			exitCode := 0
			return &exitCode, nil
		}
		if job.Status.Failed > 0 {
			exitCode := b.lastExitCode(ctx, jobName)
			return &exitCode, fmt.Errorf("job %s failed", jobName)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for job %s", jobName)
		}
		if err := sleepUntilNextPoll(ctx, deadline, jobPollInterval(time.Since(startedAt))); err != nil {
			return nil, err
		}
	}
}

func (b *Backend) lastExitCode(ctx context.Context, jobName string) int {
	selector := labels.SelectorFromSet(labels.Set{"job-name": jobName}).String()
	pods, err := b.client.CoreV1().Pods(b.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil || len(pods.Items) == 0 {
		return 1
	}
	for _, status := range pods.Items[0].Status.ContainerStatuses {
		if status.State.Terminated != nil {
			return int(status.State.Terminated.ExitCode)
		}
	}
	return 1
}

func (b *Backend) podLogs(ctx context.Context, podName string) ([]byte, error) {
	req := b.client.CoreV1().Pods(b.config.Namespace).GetLogs(podName, &corev1.PodLogOptions{})
	stream, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	return io.ReadAll(stream)
}

func (b *Backend) streamPodLogs(ctx context.Context, podName string, output chan<- string) {
	defer close(output)
	var stream io.ReadCloser
	deadline := time.Now().Add(30 * time.Second)
	for {
		req := b.client.CoreV1().Pods(b.config.Namespace).GetLogs(podName, &corev1.PodLogOptions{Follow: true})
		var err error
		stream, err = req.Stream(ctx)
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "ContainerCreating") &&
			!strings.Contains(err.Error(), "PodInitializing") &&
			!strings.Contains(err.Error(), "not available") {
			output <- fmt.Sprintf("failed to stream pod logs: %v", err)
			return
		}
		if time.Now().After(deadline) {
			output <- fmt.Sprintf("failed to stream pod logs: %v", err)
			return
		}
		select {
		case <-ctx.Done():
			output <- fmt.Sprintf("failed to stream pod logs: %v", ctx.Err())
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	defer stream.Close()
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		output <- scanner.Text()
	}
}

func (b *Backend) ensureExpectedServices(ctx context.Context, dataRoot string, procedureName string, procedure *domain.Procedure, globalPorts []domain.Port) error {
	ports := portsByName(globalPorts)
	for _, expected := range procedure.ExpectedPorts {
		port, ok := ports[expected.Name]
		if !ok {
			return fmt.Errorf("expected port %s is not defined in top-level ports", expected.Name)
		}
		service, err := serviceSpec(b.config.Namespace, dataRoot, procedureName, expected.Name, port)
		if err != nil {
			return err
		}
		if _, err := b.client.CoreV1().Services(b.config.Namespace).Create(ctx, service, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}

func (b *Backend) serviceReady(ctx context.Context, name string) (bool, int) {
	service, err := b.client.CoreV1().Services(b.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, 0
	}
	hostPort := 0
	if len(service.Spec.Ports) > 0 {
		hostPort = int(service.Spec.Ports[0].Port)
	}
	selector := labels.SelectorFromSet(labels.Set{"kubernetes.io/service-name": name}).String()
	slices, err := b.client.DiscoveryV1().EndpointSlices(b.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
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

func runtimeConsoleID(scrollID string, procedureName string) string {
	if scrollID == "" {
		return procedureName
	}
	return scrollID + "/" + procedureName
}
