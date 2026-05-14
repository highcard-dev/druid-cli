package kubernetes

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sclient "k8s.io/client-go/kubernetes"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type Backend struct {
	client         k8sclient.Interface
	restConfig     *rest.Config
	consoleManager ports.ConsoleManagerInterface
	config         Config
	hubble         HubbleClient
	jobLogRunner   func(context.Context, *batchv1.Job) ([]byte, error)
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
	logger.Log().Info("Using Kubernetes backend settings", zap.String("source", source), zap.String("namespace", config.Namespace))
	backend := &Backend{
		client:         client,
		restConfig:     restConfig,
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

func (b *Backend) SpawnPullWorker(ctx context.Context, action ports.RuntimeWorkerAction) error {
	if err := b.config.ValidateForMaterialization(); err != nil {
		return err
	}
	if action.MountPath == "" {
		action.MountPath = "/scroll"
	}
	namespace, pvc, err := parseRef(action.RootRef)
	if err != nil {
		return err
	}
	if action.Mode == ports.RuntimeWorkerModeCreate {
		if err := b.ensurePVC(ctx, pvc); err != nil {
			return err
		}
	}
	registryConfigSecret, cleanupRegistryConfig, err := b.createRegistryConfigSecret(ctx, namespace, action.Artifact+action.RuntimeID, action.RegistryCredentials)
	if err != nil {
		return err
	}
	defer cleanupRegistryConfig()
	job := workerPullJobSpec(namespace, jobName("worker-pull", action.RootRef, shortHash(string(action.Mode)+action.Artifact)), pvc, b.config.PullImage, action, b.config.RegistrySecret, registryConfigSecret, b.config.RegistryPlainHTTP)
	return b.runHelperJob(ctx, job)
}

func (b *Backend) ReadScrollFile(root string) ([]byte, error) {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return nil, err
	}
	job := readScrollJobSpec(namespace, jobName("read", root, "scroll-yaml"), pvc, b.config.HelperImage)
	return b.runJobAndLogs(context.Background(), job)
}

func (b *Backend) StartDev(ctx context.Context, action ports.RuntimeDevAction) error {
	if b.config.PullImage == "" {
		return fmt.Errorf("kubernetes dev requires --k8s-pull-image or DRUID_K8S_PULL_IMAGE")
	}
	if action.MountPath == "" {
		action.MountPath = "/scroll"
	}
	if action.Listen == "" {
		action.Listen = ":8084"
	}
	namespace, pvc, err := parseRef(action.RootRef)
	if err != nil {
		return err
	}
	sts := devStatefulSetSpec(namespace, action.RootRef, pvc, b.config.PullImage, action, b.config.RegistrySecret)
	existing, err := b.client.AppsV1().StatefulSets(namespace).Get(ctx, sts.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		if _, err := b.client.AppsV1().StatefulSets(namespace).Create(ctx, sts, metav1.CreateOptions{}); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		sts.ResourceVersion = existing.ResourceVersion
		if _, err := b.client.AppsV1().StatefulSets(namespace).Update(ctx, sts, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	service := devServiceSpec(namespace, action.RootRef, pvc)
	if err := b.reconcileService(ctx, service); err != nil {
		return err
	}
	return b.waitForStatefulSet(ctx, sts.Name)
}

func (b *Backend) StopDev(ctx context.Context, root string) error {
	namespace, _, err := parseRef(root)
	if err != nil {
		return err
	}
	propagation := metav1.DeletePropagationBackground
	_ = b.client.AppsV1().StatefulSets(namespace).Delete(ctx, devStatefulSetName(root), metav1.DeleteOptions{PropagationPolicy: &propagation})
	_ = b.client.CoreV1().Services(namespace).Delete(ctx, serviceName(root, "dev", "webdav"), metav1.DeleteOptions{})
	return nil
}

func (b *Backend) reconcileService(ctx context.Context, service *corev1.Service) error {
	existing, err := b.client.CoreV1().Services(service.Namespace).Get(ctx, service.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		_, err := b.client.CoreV1().Services(service.Namespace).Create(ctx, service, metav1.CreateOptions{})
		return err
	case err != nil:
		return err
	}
	service.ResourceVersion = existing.ResourceVersion
	service.Spec.ClusterIP = existing.Spec.ClusterIP
	service.Spec.ClusterIPs = existing.Spec.ClusterIPs
	service.Spec.IPFamilies = existing.Spec.IPFamilies
	service.Spec.IPFamilyPolicy = existing.Spec.IPFamilyPolicy
	_, err = b.client.CoreV1().Services(service.Namespace).Update(ctx, service, metav1.UpdateOptions{})
	return err
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
				if err := b.Signal(procedureName, procedure.Target, procedure.Signal, command.Root); err != nil {
					return nil, err
				}
				continue
			}
			if procedure.Image == "" {
				return nil, fmt.Errorf("kubernetes procedure %s requires image", procedureName)
			}
			if err := b.ensurePersistentProcedure(context.Background(), command.ScrollID, command.Root, procedureName, procedure, command.GlobalPorts, env); err != nil {
				return nil, err
			}
			continue
		}
		exitCode, err := b.runJobProcedure(command.ScrollID, procedureName, procedure, command.Root, command.GlobalPorts, env)
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

func (b *Backend) runJobProcedure(scrollID string, procedureName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, env map[string]string) (*int, error) {
	if procedure.IsSignal() {
		return nil, b.Signal(procedureName, procedure.Target, procedure.Signal, root)
	}
	if procedure.Image == "" {
		return nil, fmt.Errorf("kubernetes procedure %s requires image", procedureName)
	}
	ctx := context.Background()
	if err := b.ensureExpectedServices(ctx, root, procedureName, procedure, globalPorts); err != nil {
		return nil, err
	}
	job, err := procedureJobSpec(b.config.Namespace, root, procedureName, procedure, env, b.config.RegistrySecret)
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
		return b.attachToProcedure(root, procedureName, data)
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

func (b *Backend) ensurePersistentProcedure(ctx context.Context, scrollID string, root string, procedureName string, procedure *domain.Procedure, globalPorts []domain.Port, env map[string]string) error {
	if err := b.ensureExpectedServices(ctx, root, procedureName, procedure, globalPorts); err != nil {
		return err
	}
	statefulSet, err := procedureStatefulSetSpec(b.config.Namespace, root, procedureName, procedure, env, b.config.RegistrySecret)
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
		return b.attachToProcedure(root, procedureName, data)
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

func (b *Backend) ExpectedPorts(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimePortStatus, error) {
	_, pvc, err := parseRef(root)
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
				serviceReady, hostPort := b.serviceReady(context.Background(), serviceName(root, procedureName, expectedPort.Name))
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
				selector := map[string]string{
					labelManagedBy: "druid",
					labelComponent: "runtime",
					labelScrollID:  dnsLabel(pvc),
				}
				if len(procedure.ExpectedPorts) == 1 {
					selector[labelPortName] = dnsLabel(expectedPort.Name)
				} else {
					selector[labelProcedure] = dnsLabel(procedureName)
				}
				targets = append(targets, domain.RuntimeRoutingTarget{
					Name:        expectedPort.Name,
					Procedure:   procedureName,
					PortName:    expectedPort.Name,
					Port:        port.Port,
					Protocol:    normalizeProtocol(port.Protocol),
					Namespace:   namespace,
					ServiceName: serviceName(root, procedureName, expectedPort.Name),
					ServicePort: port.Port,
					Selector:    selector,
				})
			}
		}
	}
	return targets, nil
}

func (b *Backend) StopRuntime(root string) error {
	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{PropagationPolicy: &propagation}
	if err := b.deleteRuntimeJobs(context.Background(), root, options); err != nil {
		return err
	}
	if err := b.deleteRuntimeStatefulSets(context.Background(), root, options); err != nil {
		return err
	}
	return b.deleteRuntimePodsByScroll(context.Background(), root, options)
}

func (b *Backend) DeleteRuntime(root string, purgeData bool) error {
	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{PropagationPolicy: &propagation}
	if err := b.StopRuntime(root); err != nil {
		return err
	}
	if err := b.deleteRuntimeServices(context.Background(), root, options); err != nil {
		return err
	}
	if purgeData {
		namespace, pvc, err := parseRef(root)
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

func (b *Backend) BackupRuntime(ctx context.Context, root string, artifact string, registryCredentials []domain.RegistryCredential) error {
	if artifact == "" {
		return fmt.Errorf("backup artifact is required")
	}
	if b.config.PullImage == "" {
		return b.config.ValidateForMaterialization()
	}
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return err
	}
	registryConfigSecret, cleanupRegistryConfig, err := b.createRegistryConfigSecret(ctx, namespace, artifact+root, registryCredentials)
	if err != nil {
		return err
	}
	defer cleanupRegistryConfig()
	job := backupJobSpec(namespace, jobName("backup", root, shortHash(artifact)), pvc, b.config.PullImage, artifact, b.config.RegistrySecret, registryConfigSecret, b.config.RegistryPlainHTTP)
	return b.runHelperJob(ctx, job)
}

func (b *Backend) RestoreRuntime(ctx context.Context, root string, artifact string, registryCredentials []domain.RegistryCredential) error {
	if artifact == "" {
		return fmt.Errorf("restore artifact is required")
	}
	if err := b.config.ValidateForMaterialization(); err != nil {
		return err
	}
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return err
	}
	stagePVC := stagingPVCName("restore:" + root + ":" + artifact)
	if err := b.ensurePVC(ctx, stagePVC); err != nil {
		return err
	}
	defer b.client.CoreV1().PersistentVolumeClaims(namespace).Delete(context.Background(), stagePVC, metav1.DeleteOptions{})
	registryConfigSecret, cleanupRegistryConfig, err := b.createRegistryConfigSecret(ctx, namespace, artifact+root, registryCredentials)
	if err != nil {
		return err
	}
	defer cleanupRegistryConfig()
	pullJob := pullJobSpec(namespace, jobName("restore-pull", ref(namespace, stagePVC), shortHash(artifact)), stagePVC, b.config.PullImage, artifact, b.config.RegistrySecret, registryConfigSecret, b.config.RegistryPlainHTTP)
	if err := b.runHelperJob(ctx, pullJob); err != nil {
		return err
	}
	if err := b.StopRuntime(root); err != nil {
		return err
	}
	restoreJob := replacePVCJobSpec(namespace, jobName("restore-copy", root, shortHash(artifact)), stagePVC, pvc, b.config.HelperImage)
	return b.runHelperJob(ctx, restoreJob)
}

func (b *Backend) Attach(commandName string, data string) error {
	pods, err := b.client.CoreV1().Pods(b.config.Namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{labelProcedure: dnsLabel(commandName)}).String(),
	})
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			return b.attachToPod(context.Background(), pod.Name, data)
		}
	}
	return fmt.Errorf("no running pod found for console %s", commandName)
}

func (b *Backend) attachToProcedure(root string, procedureName string, data string) error {
	_, pvc, err := parseRef(root)
	if err != nil {
		return err
	}
	selector := baseLabels(pvc)
	selector[labelProcedure] = dnsLabel(procedureName)
	podName, err := b.waitForPodBySelector(context.Background(), labels.SelectorFromSet(selector).String())
	if err != nil {
		return err
	}
	return b.attachToPod(context.Background(), podName, data)
}

func (b *Backend) attachToPod(ctx context.Context, podName string, data string) error {
	req := b.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(b.config.Namespace).
		Name(podName).
		SubResource("attach").
		VersionedParams(&corev1.PodAttachOptions{
			Container: "main",
			Stdin:     true,
			Stdout:    false,
			Stderr:    false,
			TTY:       false,
		}, k8sscheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(b.restConfig, "POST", req.URL())
	if err != nil {
		return err
	}
	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin: strings.NewReader(data),
	})
}

func (b *Backend) Signal(_ string, target string, signal string, root string) error {
	if target == "" {
		return nil
	}
	switch signal {
	case "", "SIGTERM", "TERM":
		propagation := metav1.DeletePropagationBackground
		return b.deleteRuntimeWorkload(context.Background(), root, target, metav1.DeleteOptions{PropagationPolicy: &propagation})
	case "SIGKILL", "KILL":
		grace := int64(0)
		propagation := metav1.DeletePropagationBackground
		return b.deleteRuntimeWorkload(context.Background(), root, target, metav1.DeleteOptions{GracePeriodSeconds: &grace, PropagationPolicy: &propagation})
	default:
		return fmt.Errorf("kubernetes signal %s is unsupported without pod exec", signal)
	}
}

func (b *Backend) deleteRuntimeWorkload(ctx context.Context, root string, target string, options metav1.DeleteOptions) error {
	jobErr := b.client.BatchV1().Jobs(b.config.Namespace).Delete(ctx, jobName("proc", root, target), options)
	if apierrors.IsNotFound(jobErr) {
		jobErr = nil
	}
	statefulSetErr := b.client.AppsV1().StatefulSets(b.config.Namespace).Delete(ctx, statefulSetName(root, target), options)
	if apierrors.IsNotFound(statefulSetErr) {
		statefulSetErr = nil
	}
	podErr := b.deleteRuntimePods(ctx, root, target, options)
	if jobErr != nil {
		return jobErr
	}
	if statefulSetErr != nil {
		return statefulSetErr
	}
	return podErr
}

func (b *Backend) deleteRuntimePods(ctx context.Context, root string, target string, options metav1.DeleteOptions) error {
	_, pvc, err := parseRef(root)
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

func (b *Backend) deleteRuntimeJobs(ctx context.Context, root string, options metav1.DeleteOptions) error {
	return b.deleteRuntimeObjects(ctx, root, func(name string) error {
		err := b.client.BatchV1().Jobs(b.config.Namespace).Delete(ctx, name, options)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}, "jobs")
}

func (b *Backend) deleteRuntimeStatefulSets(ctx context.Context, root string, options metav1.DeleteOptions) error {
	return b.deleteRuntimeObjects(ctx, root, func(name string) error {
		err := b.client.AppsV1().StatefulSets(b.config.Namespace).Delete(ctx, name, options)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}, "statefulsets")
}

func (b *Backend) deleteRuntimeServices(ctx context.Context, root string, options metav1.DeleteOptions) error {
	return b.deleteRuntimeObjects(ctx, root, func(name string) error {
		err := b.client.CoreV1().Services(b.config.Namespace).Delete(ctx, name, options)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}, "services")
}

func (b *Backend) deleteRuntimeObjects(ctx context.Context, root string, deleteOne func(name string) error, kind string) error {
	_, pvc, err := parseRef(root)
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

func (b *Backend) deleteRuntimePodsByScroll(ctx context.Context, root string, options metav1.DeleteOptions) error {
	_, pvc, err := parseRef(root)
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

func (b *Backend) createRegistryConfigSecret(ctx context.Context, namespace string, seed string, credentials []domain.RegistryCredential) (string, func(), error) {
	if len(credentials) == 0 {
		return "", func() {}, nil
	}
	data, err := json.Marshal(struct {
		Registries []domain.RegistryCredential `json:"registries"`
	}{Registries: credentials})
	if err != nil {
		return "", nil, err
	}
	name := dnsLabel("druid-registry-" + shortHash(fmt.Sprintf("%s-%d", seed, time.Now().UnixNano())))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				labelManagedBy: "druid",
				labelComponent: "registry-auth",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{registryConfigSecretKey: data},
	}
	if _, err := b.client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return "", nil, err
	}
	cleanup := func() {
		deleteCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = b.client.CoreV1().Secrets(namespace).Delete(deleteCtx, name, metav1.DeleteOptions{})
	}
	return name, cleanup, nil
}

func (b *Backend) runHelperJob(ctx context.Context, job *batchv1.Job) error {
	_, err := b.runJobAndLogs(ctx, job)
	return err
}

func (b *Backend) runJobAndLogs(ctx context.Context, job *batchv1.Job) ([]byte, error) {
	if b.jobLogRunner != nil {
		return b.jobLogRunner(ctx, job)
	}
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

func (b *Backend) ensureExpectedServices(ctx context.Context, root string, procedureName string, procedure *domain.Procedure, globalPorts []domain.Port) error {
	ports := portsByName(globalPorts)
	for _, expected := range procedure.ExpectedPorts {
		port, ok := ports[expected.Name]
		if !ok {
			return fmt.Errorf("expected port %s is not defined in top-level ports", expected.Name)
		}
		service, err := serviceSpec(b.config.Namespace, root, procedureName, expected.Name, port)
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
