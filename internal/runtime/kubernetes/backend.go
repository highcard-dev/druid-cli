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

func (b *Backend) RootRef(id string, namespace string) string {
	if namespace == "" {
		namespace = b.config.Namespace
	}
	return ref(namespace, dataPVCName(id))
}

func (b *Backend) SpawnPullWorker(ctx context.Context, action ports.RuntimeWorkerAction) error {
	if err := b.config.ValidateForMaterialization(); err != nil {
		logger.Log().Error("Kubernetes pull worker config invalid", zap.String("runtime_id", action.RuntimeID), zap.Error(err))
		return err
	}
	if action.MountPath == "" {
		action.MountPath = "/scroll"
	}
	namespace, pvc, err := parseRef(action.RootRef)
	if err != nil {
		logger.Log().Error("Kubernetes pull worker root ref invalid", zap.String("runtime_id", action.RuntimeID), zap.String("root_ref", action.RootRef), zap.Error(err))
		return err
	}
	logger.Log().Info("Spawning Kubernetes pull worker",
		zap.String("runtime_id", action.RuntimeID),
		zap.String("mode", string(action.Mode)),
		zap.String("namespace", namespace),
		zap.String("pvc", pvc),
		zap.String("artifact", action.Artifact),
	)
	logger.Log().Debug("Kubernetes pull worker details",
		zap.String("runtime_id", action.RuntimeID),
		zap.String("root_ref", action.RootRef),
		zap.String("mount_path", action.MountPath),
		zap.String("pull_image", b.config.PullImage),
		zap.Bool("registry_plain_http", b.config.RegistryPlainHTTP),
		zap.Bool("has_registry_credentials", len(action.RegistryCredentials) > 0),
	)
	if action.Mode == ports.RuntimeWorkerModeCreate {
		if err := b.ensurePVC(ctx, namespace, pvc); err != nil {
			logger.Log().Error("Failed to ensure runtime PVC for pull worker", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Error(err))
			return err
		}
	}
	registryConfigSecret, cleanupRegistryConfig, err := b.createRegistryConfigSecret(ctx, namespace, action.Artifact+action.RuntimeID, action.RegistryCredentials)
	if err != nil {
		logger.Log().Error("Failed to create registry config secret for pull worker", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.Error(err))
		return err
	}
	defer cleanupRegistryConfig()
	job := workerPullJobSpec(namespace, jobName("worker-pull", action.RootRef, shortHash(string(action.Mode)+action.Artifact)), pvc, b.config.PullImage, action, b.config.RegistrySecret, registryConfigSecret, b.config.RegistryPlainHTTP)
	logger.Log().Debug("Kubernetes pull worker job built", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("job", job.Name))
	if err := b.runHelperJob(ctx, job); err != nil {
		logger.Log().Error("Kubernetes pull worker failed", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("job", job.Name), zap.Error(err))
		return err
	}
	logger.Log().Info("Kubernetes pull worker completed", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("job", job.Name))
	return nil
}

func (b *Backend) ReadScrollFile(root string) ([]byte, error) {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot read scroll.yaml from invalid Kubernetes root", zap.String("root", root), zap.Error(err))
		return nil, err
	}
	job := readScrollJobSpec(namespace, jobName("read", root, "scroll-yaml"), pvc, b.config.HelperImage)
	logger.Log().Debug("Reading scroll.yaml through Kubernetes helper job", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("job", job.Name))
	logs, err := b.runJobAndLogs(context.Background(), job)
	if err != nil {
		logger.Log().Error("Failed to read scroll.yaml through Kubernetes helper job", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("job", job.Name), zap.Error(err))
		return logs, err
	}
	logger.Log().Debug("Read scroll.yaml through Kubernetes helper job", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("job", job.Name), zap.Int("bytes", len(logs)))
	return logs, nil
}

func (b *Backend) StartDev(ctx context.Context, action ports.RuntimeDevAction) error {
	if b.config.PullImage == "" {
		logger.Log().Error("Cannot start Kubernetes dev server without pull image", zap.String("runtime_id", action.RuntimeID), zap.String("root_ref", action.RootRef))
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
		logger.Log().Error("Kubernetes dev root ref invalid", zap.String("runtime_id", action.RuntimeID), zap.String("root_ref", action.RootRef), zap.Error(err))
		return err
	}
	logger.Log().Info("Starting Kubernetes dev server",
		zap.String("runtime_id", action.RuntimeID),
		zap.String("namespace", namespace),
		zap.String("pvc", pvc),
		zap.String("listen", action.Listen),
	)
	logger.Log().Debug("Kubernetes dev server details",
		zap.String("runtime_id", action.RuntimeID),
		zap.String("root_ref", action.RootRef),
		zap.String("mount_path", action.MountPath),
		zap.Strings("watch_paths", action.WatchPaths),
		zap.Strings("commands", action.HotReloadCommands),
		zap.String("image", b.config.PullImage),
	)
	sts := devStatefulSetSpec(namespace, action.RootRef, pvc, b.config.PullImage, action, b.config.RegistrySecret)
	existing, err := b.client.AppsV1().StatefulSets(namespace).Get(ctx, sts.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		logger.Log().Info("Creating Kubernetes dev StatefulSet", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name))
		if _, err := b.client.AppsV1().StatefulSets(namespace).Create(ctx, sts, metav1.CreateOptions{}); err != nil {
			logger.Log().Error("Failed to create Kubernetes dev StatefulSet", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name), zap.Error(err))
			return err
		}
	case err != nil:
		logger.Log().Error("Failed to get Kubernetes dev StatefulSet", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name), zap.Error(err))
		return err
	default:
		logger.Log().Info("Updating Kubernetes dev StatefulSet", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name), zap.String("resource_version", existing.ResourceVersion))
		sts.ResourceVersion = existing.ResourceVersion
		if _, err := b.client.AppsV1().StatefulSets(namespace).Update(ctx, sts, metav1.UpdateOptions{}); err != nil {
			logger.Log().Error("Failed to update Kubernetes dev StatefulSet", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name), zap.Error(err))
			return err
		}
	}
	service := devServiceSpec(namespace, action.RootRef, pvc)
	if err := b.reconcileService(ctx, service); err != nil {
		logger.Log().Error("Failed to reconcile Kubernetes dev Service", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("service", service.Name), zap.Error(err))
		return err
	}
	if err := b.waitForStatefulSet(ctx, namespace, sts.Name); err != nil {
		logger.Log().Error("Kubernetes dev StatefulSet did not become ready", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name), zap.Error(err))
		return err
	}
	logger.Log().Info("Kubernetes dev server ready", zap.String("runtime_id", action.RuntimeID), zap.String("namespace", namespace), zap.String("statefulset", sts.Name), zap.String("service", service.Name))
	return nil
}

func (b *Backend) StopDev(ctx context.Context, root string) error {
	namespace, _, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot stop Kubernetes dev server for invalid root", zap.String("root", root), zap.Error(err))
		return err
	}
	propagation := metav1.DeletePropagationBackground
	statefulSetName := devStatefulSetName(root)
	serviceName := serviceName(root, "dev", "webdav")
	logger.Log().Info("Stopping Kubernetes dev server", zap.String("namespace", namespace), zap.String("statefulset", statefulSetName), zap.String("service", serviceName))
	if err := b.client.AppsV1().StatefulSets(namespace).Delete(ctx, statefulSetName, metav1.DeleteOptions{PropagationPolicy: &propagation}); err != nil && !apierrors.IsNotFound(err) {
		logger.Log().Warn("Failed to delete Kubernetes dev StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", statefulSetName), zap.Error(err))
	}
	if err := b.client.CoreV1().Services(namespace).Delete(ctx, serviceName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		logger.Log().Warn("Failed to delete Kubernetes dev Service", zap.String("namespace", namespace), zap.String("service", serviceName), zap.Error(err))
	}
	return nil
}

func (b *Backend) reconcileService(ctx context.Context, service *corev1.Service) error {
	logger.Log().Debug("Reconciling Kubernetes Service", zap.String("namespace", service.Namespace), zap.String("service", service.Name), zap.Any("selector", service.Spec.Selector), zap.Int("ports", len(service.Spec.Ports)))
	existing, err := b.client.CoreV1().Services(service.Namespace).Get(ctx, service.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		logger.Log().Info("Creating Kubernetes Service", zap.String("namespace", service.Namespace), zap.String("service", service.Name))
		_, err := b.client.CoreV1().Services(service.Namespace).Create(ctx, service, metav1.CreateOptions{})
		if err != nil {
			logger.Log().Error("Failed to create Kubernetes Service", zap.String("namespace", service.Namespace), zap.String("service", service.Name), zap.Error(err))
		}
		return err
	case err != nil:
		logger.Log().Error("Failed to get Kubernetes Service", zap.String("namespace", service.Namespace), zap.String("service", service.Name), zap.Error(err))
		return err
	}
	logger.Log().Info("Updating Kubernetes Service", zap.String("namespace", service.Namespace), zap.String("service", service.Name), zap.String("resource_version", existing.ResourceVersion))
	service.ResourceVersion = existing.ResourceVersion
	service.Spec.ClusterIP = existing.Spec.ClusterIP
	service.Spec.ClusterIPs = existing.Spec.ClusterIPs
	service.Spec.IPFamilies = existing.Spec.IPFamilies
	service.Spec.IPFamilyPolicy = existing.Spec.IPFamilyPolicy
	_, err = b.client.CoreV1().Services(service.Namespace).Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		logger.Log().Error("Failed to update Kubernetes Service", zap.String("namespace", service.Namespace), zap.String("service", service.Name), zap.Error(err))
	}
	return err
}

func (b *Backend) RunCommand(command ports.RuntimeCommand) (*int, error) {
	if command.Command == nil {
		err := fmt.Errorf("kubernetes command %s has no instruction set", command.Name)
		logger.Log().Error("Cannot run Kubernetes command", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.Error(err))
		return nil, err
	}
	logger.Log().Info("Running Kubernetes command",
		zap.String("scroll_id", command.ScrollID),
		zap.String("command", command.Name),
		zap.String("run_mode", string(command.Command.Run)),
		zap.String("root", command.Root),
		zap.Int("procedures", len(command.Command.Procedures)),
	)
	portUse := expectedPortUse(command.Command)
	for idx, procedure := range command.Command.Procedures {
		if procedure == nil {
			logger.Log().Warn("Skipping nil Kubernetes procedure", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.Int("procedure_index", idx))
			continue
		}
		procedureName := domain.ProcedureName(command.Name, idx, procedure)
		resourceName := procedureResourceName(command.Root, command.Name, idx)
		env := command.ProcedureEnv[procedureName]
		if env == nil {
			env = procedure.Env
		}
		logger.Log().Debug("Kubernetes procedure selected",
			zap.String("scroll_id", command.ScrollID),
			zap.String("command", command.Name),
			zap.String("procedure", procedureName),
			zap.String("resource", resourceName),
			zap.String("run_mode", string(command.Command.Run)),
			zap.String("image", procedure.Image),
			zap.Bool("persistent", command.Command.Run == domain.RunModePersistent),
			zap.Bool("signal", procedure.IsSignal()),
			zap.Bool("ignore_failure", procedure.IgnoreFailure),
			zap.Int("env_count", len(env)),
			zap.Int("expected_ports", len(procedure.ExpectedPorts)),
			zap.Int("mounts", len(procedure.Mounts)),
		)
		if command.Command.Run == domain.RunModePersistent {
			if procedure.IsSignal() {
				if err := b.Signal(procedureName, procedure.Target, procedure.Signal, command.Root); err != nil {
					logger.Log().Error("Kubernetes signal procedure failed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.String("target", procedure.Target), zap.String("signal", procedure.Signal), zap.Error(err))
					return nil, err
				}
				logger.Log().Info("Kubernetes signal procedure completed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.String("target", procedure.Target), zap.String("signal", procedure.Signal))
				continue
			}
			if procedure.Image == "" {
				err := fmt.Errorf("kubernetes procedure %s requires image", procedureName)
				logger.Log().Error("Kubernetes persistent procedure missing image", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Error(err))
				return nil, err
			}
			if err := b.ensurePersistentProcedure(context.Background(), command.ScrollID, command.Root, command.Name, procedureName, resourceName, procedure, command.GlobalPorts, env, portUse); err != nil {
				logger.Log().Error("Kubernetes persistent procedure failed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Error(err))
				return nil, err
			}
			continue
		}
		exitCode, err := b.runJobProcedure(command.ScrollID, command.Name, procedureName, resourceName, procedure, command.Root, command.GlobalPorts, env, portUse)
		if err != nil {
			logger.Log().Error("Kubernetes job procedure failed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Any("exit_code", exitCode), zap.Error(err))
			return exitCode, err
		}
		if exitCode != nil && *exitCode != 0 {
			if procedure.IgnoreFailure {
				logger.Log().Warn("Kubernetes job procedure failed but failure is ignored", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Int("exit_code", *exitCode))
				continue
			}
			logger.Log().Warn("Kubernetes command stopped after non-zero procedure exit", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Int("exit_code", *exitCode))
			return exitCode, nil
		}
		if exitCode != nil {
			logger.Log().Info("Kubernetes job procedure completed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Int("exit_code", *exitCode))
		}
	}
	logger.Log().Info("Kubernetes command completed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name))
	return nil, nil
}

func (b *Backend) runJobProcedure(scrollID string, commandName string, procedureName string, resourceName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, env map[string]string, portUse map[string]int) (*int, error) {
	if procedure.IsSignal() {
		logger.Log().Info("Running Kubernetes signal procedure", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("target", procedure.Target), zap.String("signal", procedure.Signal))
		if err := b.Signal(procedureName, procedure.Target, procedure.Signal, root); err != nil {
			logger.Log().Error("Kubernetes signal procedure failed", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.Error(err))
			return nil, err
		}
		return nil, nil
	}
	if procedure.Image == "" {
		err := fmt.Errorf("kubernetes procedure %s requires image", procedureName)
		logger.Log().Error("Kubernetes job procedure missing image", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.Error(err))
		return nil, err
	}
	ctx := context.Background()
	if err := b.ensureExpectedServices(ctx, root, commandName, procedureName, procedure, globalPorts, portUse); err != nil {
		logger.Log().Error("Failed to reconcile Kubernetes procedure Services", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.Error(err))
		return nil, err
	}
	namespace, _, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Kubernetes job procedure root ref invalid", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("root", root), zap.Error(err))
		return nil, err
	}
	job, err := procedureJobSpec(namespace, root, commandName, procedureName, resourceName, procedure, env, b.config.RegistrySecret)
	if err != nil {
		logger.Log().Error("Failed to build Kubernetes procedure Job", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.Error(err))
		return nil, err
	}
	logger.Log().Info("Starting Kubernetes job procedure",
		zap.String("scroll_id", scrollID),
		zap.String("command", commandName),
		zap.String("procedure", procedureName),
		zap.String("namespace", namespace),
		zap.String("job", job.Name),
	)
	logger.Log().Debug("Kubernetes job procedure details",
		zap.String("scroll_id", scrollID),
		zap.String("command", commandName),
		zap.String("procedure", procedureName),
		zap.String("resource", resourceName),
		zap.String("image", procedure.Image),
		zap.Int("env_count", len(env)),
		zap.Int("expected_ports", len(procedure.ExpectedPorts)),
		zap.Int("mounts", len(procedure.Mounts)),
	)
	createdJob, err := b.createFreshJob(ctx, job)
	if err != nil {
		logger.Log().Error("Failed to create Kubernetes job procedure", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", job.Name), zap.Error(err))
		return nil, err
	}
	output := make(chan string, 100)
	consoleID := runtimeConsoleID(scrollID, procedureName)
	console, doneChan := b.consoleManager.AddConsoleWithChannel(consoleID, domain.ConsoleTypeContainer, "stdin", output)
	console.WriteInput = func(data string) error {
		return b.attachToProcedure(root, procedureName, data)
	}
	streamStarted := false
	jobName := createdJob.Name
	podName, err := b.waitForJobPod(ctx, namespace, jobName, string(createdJob.UID))
	if err == nil {
		streamStarted = true
		logger.Log().Debug("Streaming Kubernetes job procedure logs", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.String("pod", podName), zap.String("console_id", consoleID))
		go b.streamPodLogs(ctx, namespace, podName, output)
	} else {
		logger.Log().Warn("Could not find Kubernetes job pod before wait; console logs may be empty", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Error(err))
	}
	exitCode, err := b.waitForJob(ctx, namespace, jobName)
	if exitCode != nil {
		console.MarkExited(*exitCode)
	}
	if !streamStarted {
		close(output)
	}
	<-doneChan
	if err != nil {
		if exitCode != nil && *exitCode == 0 {
			b.deleteFinishedJob(context.Background(), namespace, jobName)
		}
		if exitCode != nil && *exitCode != 0 {
			logger.Log().Warn("Keeping failed Kubernetes job procedure for debugging", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
		}
		logger.Log().Error("Kubernetes job procedure ended with error", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Any("exit_code", exitCode), zap.Error(err))
		return exitCode, err
	}
	if exitCode != nil && *exitCode == 0 {
		b.deleteFinishedJob(context.Background(), namespace, jobName)
		logger.Log().Info("Kubernetes job procedure exited", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
	} else if exitCode != nil {
		logger.Log().Warn("Keeping failed Kubernetes job procedure for debugging", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
	}
	return exitCode, nil
}

func (b *Backend) ensurePersistentProcedure(ctx context.Context, scrollID string, root string, commandName string, procedureName string, resourceName string, procedure *domain.Procedure, globalPorts []domain.Port, env map[string]string, portUse map[string]int) error {
	if err := b.ensureExpectedServices(ctx, root, commandName, procedureName, procedure, globalPorts, portUse); err != nil {
		logger.Log().Error("Failed to reconcile Kubernetes persistent procedure Services", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.Error(err))
		return err
	}
	namespace, _, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Kubernetes persistent procedure root ref invalid", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("root", root), zap.Error(err))
		return err
	}
	statefulSet, err := procedureStatefulSetSpec(namespace, root, commandName, procedureName, resourceName, procedure, env, b.config.RegistrySecret)
	if err != nil {
		logger.Log().Error("Failed to build Kubernetes persistent procedure StatefulSet", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.Error(err))
		return err
	}
	logger.Log().Info("Reconciling Kubernetes persistent procedure",
		zap.String("scroll_id", scrollID),
		zap.String("command", commandName),
		zap.String("procedure", procedureName),
		zap.String("namespace", namespace),
		zap.String("statefulset", statefulSet.Name),
	)
	logger.Log().Debug("Kubernetes persistent procedure details",
		zap.String("scroll_id", scrollID),
		zap.String("command", commandName),
		zap.String("procedure", procedureName),
		zap.String("resource", resourceName),
		zap.String("image", procedure.Image),
		zap.Int("env_count", len(env)),
		zap.Int("expected_ports", len(procedure.ExpectedPorts)),
		zap.Int("mounts", len(procedure.Mounts)),
	)
	existing, err := b.client.AppsV1().StatefulSets(namespace).Get(ctx, statefulSet.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		logger.Log().Info("Creating Kubernetes persistent procedure StatefulSet", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name))
		if _, err := b.client.AppsV1().StatefulSets(namespace).Create(ctx, statefulSet, metav1.CreateOptions{}); err != nil {
			logger.Log().Error("Failed to create Kubernetes persistent procedure StatefulSet", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
			return err
		}
	case err != nil:
		logger.Log().Error("Failed to get Kubernetes persistent procedure StatefulSet", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
		return err
	default:
		logger.Log().Info("Updating Kubernetes persistent procedure StatefulSet", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.String("resource_version", existing.ResourceVersion))
		statefulSet.ResourceVersion = existing.ResourceVersion
		if _, err := b.client.AppsV1().StatefulSets(namespace).Update(ctx, statefulSet, metav1.UpdateOptions{}); err != nil {
			logger.Log().Error("Failed to update Kubernetes persistent procedure StatefulSet", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
			return err
		}
	}
	output := make(chan string, 100)
	console, _ := b.consoleManager.AddConsoleWithChannel(runtimeConsoleID(scrollID, procedureName), domain.ConsoleTypeContainer, "stdin", output)
	console.WriteInput = func(data string) error {
		return b.attachToProcedure(root, procedureName, data)
	}
	if err := b.waitForStatefulSet(ctx, namespace, statefulSet.Name); err != nil {
		close(output)
		logger.Log().Error("Kubernetes persistent procedure did not become ready", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
		return err
	}
	logger.Log().Info("Kubernetes persistent procedure ready", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name))
	go func() {
		podName, err := b.waitForPodBySelector(context.Background(), namespace, labels.SelectorFromSet(labels.Set{
			labelScrollID:  statefulSet.Labels[labelScrollID],
			labelProcedure: statefulSet.Labels[labelProcedure],
		}).String())
		if err != nil {
			logger.Log().Warn("Failed to find Kubernetes persistent procedure pod for logs", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
			output <- fmt.Sprintf("failed to find StatefulSet pod logs: %v", err)
			close(output)
			return
		}
		logger.Log().Debug("Streaming Kubernetes persistent procedure logs", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("pod", podName))
		b.streamPodLogs(context.Background(), namespace, podName, output)
	}()
	return nil
}

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
				targets = append(targets, domain.RuntimeRoutingTarget{
					Name:        expectedPort.Name,
					Procedure:   procedureName,
					PortName:    expectedPort.Name,
					Port:        port.Port,
					Protocol:    normalizeProtocol(port.Protocol),
					Namespace:   namespace,
					ServiceName: serviceName(root, serviceProcedure, expectedPort.Name),
					ServicePort: port.Port,
					Selector:    serviceSelector(pvc, commandName, procedureName, expectedPort.Name, portUse),
				})
			}
		}
	}
	return targets, nil
}

func (b *Backend) StopRuntime(root string) error {
	logger.Log().Info("Stopping Kubernetes runtime", zap.String("root", root))
	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{PropagationPolicy: &propagation}
	if err := b.deleteRuntimeJobs(context.Background(), root, options); err != nil {
		logger.Log().Error("Failed to delete Kubernetes runtime jobs", zap.String("root", root), zap.Error(err))
		return err
	}
	if err := b.deleteRuntimeStatefulSets(context.Background(), root, options); err != nil {
		logger.Log().Error("Failed to delete Kubernetes runtime StatefulSets", zap.String("root", root), zap.Error(err))
		return err
	}
	if err := b.deleteRuntimePodsByScroll(context.Background(), root, options); err != nil {
		logger.Log().Error("Failed to delete Kubernetes runtime pods", zap.String("root", root), zap.Error(err))
		return err
	}
	logger.Log().Info("Stopped Kubernetes runtime", zap.String("root", root))
	return nil
}

func (b *Backend) DeleteRuntime(root string, purgeData bool) error {
	logger.Log().Info("Deleting Kubernetes runtime", zap.String("root", root), zap.Bool("purge_data", purgeData))
	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{PropagationPolicy: &propagation}
	if err := b.StopRuntime(root); err != nil {
		return err
	}
	if err := b.deleteRuntimeServices(context.Background(), root, options); err != nil {
		logger.Log().Error("Failed to delete Kubernetes runtime Services", zap.String("root", root), zap.Error(err))
		return err
	}
	if purgeData {
		namespace, pvc, err := parseRef(root)
		if err != nil {
			logger.Log().Error("Cannot purge Kubernetes runtime data for invalid root", zap.String("root", root), zap.Error(err))
			return err
		}
		logger.Log().Info("Deleting Kubernetes runtime PVC", zap.String("namespace", namespace), zap.String("pvc", pvc))
		err = b.client.CoreV1().PersistentVolumeClaims(namespace).Delete(context.Background(), pvc, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Log().Error("Failed to delete Kubernetes runtime PVC", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Error(err))
			return err
		}
	}
	logger.Log().Info("Deleted Kubernetes runtime", zap.String("root", root), zap.Bool("purge_data", purgeData))
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
		logger.Log().Error("Cannot backup Kubernetes runtime for invalid root", zap.String("root", root), zap.String("artifact", artifact), zap.Error(err))
		return err
	}
	logger.Log().Info("Backing up Kubernetes runtime", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("artifact", artifact))
	registryConfigSecret, cleanupRegistryConfig, err := b.createRegistryConfigSecret(ctx, namespace, artifact+root, registryCredentials)
	if err != nil {
		logger.Log().Error("Failed to create registry config secret for Kubernetes backup", zap.String("namespace", namespace), zap.String("artifact", artifact), zap.Error(err))
		return err
	}
	defer cleanupRegistryConfig()
	job := backupJobSpec(namespace, jobName("backup", root, shortHash(artifact)), pvc, b.config.PullImage, artifact, b.config.RegistrySecret, registryConfigSecret, b.config.RegistryPlainHTTP)
	if err := b.runHelperJob(ctx, job); err != nil {
		logger.Log().Error("Kubernetes runtime backup failed", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("artifact", artifact), zap.String("job", job.Name), zap.Error(err))
		return err
	}
	logger.Log().Info("Kubernetes runtime backup completed", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("artifact", artifact), zap.String("job", job.Name))
	return nil
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
		logger.Log().Error("Cannot restore Kubernetes runtime for invalid root", zap.String("root", root), zap.String("artifact", artifact), zap.Error(err))
		return err
	}
	logger.Log().Info("Restoring Kubernetes runtime", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("artifact", artifact))
	stagePVC := stagingPVCName("restore:" + root + ":" + artifact)
	if err := b.ensurePVC(ctx, namespace, stagePVC); err != nil {
		logger.Log().Error("Failed to create Kubernetes restore staging PVC", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("stage_pvc", stagePVC), zap.Error(err))
		return err
	}
	defer func() {
		if err := b.client.CoreV1().PersistentVolumeClaims(namespace).Delete(context.Background(), stagePVC, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			logger.Log().Warn("Failed to delete Kubernetes restore staging PVC", zap.String("namespace", namespace), zap.String("stage_pvc", stagePVC), zap.Error(err))
		}
	}()
	registryConfigSecret, cleanupRegistryConfig, err := b.createRegistryConfigSecret(ctx, namespace, artifact+root, registryCredentials)
	if err != nil {
		logger.Log().Error("Failed to create registry config secret for Kubernetes restore", zap.String("namespace", namespace), zap.String("artifact", artifact), zap.Error(err))
		return err
	}
	defer cleanupRegistryConfig()
	pullJob := pullJobSpec(namespace, jobName("restore-pull", ref(namespace, stagePVC), shortHash(artifact)), stagePVC, b.config.PullImage, artifact, b.config.RegistrySecret, registryConfigSecret, b.config.RegistryPlainHTTP)
	if err := b.runHelperJob(ctx, pullJob); err != nil {
		logger.Log().Error("Kubernetes restore pull failed", zap.String("namespace", namespace), zap.String("artifact", artifact), zap.String("job", pullJob.Name), zap.Error(err))
		return err
	}
	if err := b.StopRuntime(root); err != nil {
		logger.Log().Error("Failed to stop Kubernetes runtime before restore", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Error(err))
		return err
	}
	restoreJob := replacePVCJobSpec(namespace, jobName("restore-copy", root, shortHash(artifact)), stagePVC, pvc, b.config.HelperImage)
	if err := b.runHelperJob(ctx, restoreJob); err != nil {
		logger.Log().Error("Kubernetes restore copy failed", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("stage_pvc", stagePVC), zap.String("job", restoreJob.Name), zap.Error(err))
		return err
	}
	logger.Log().Info("Kubernetes runtime restore completed", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("artifact", artifact))
	return nil
}

func (b *Backend) Attach(commandName string, data string) error {
	logger.Log().Debug("Attaching to Kubernetes procedure by command name", zap.String("command", commandName), zap.Int("bytes", len(data)))
	pods, err := b.client.CoreV1().Pods(b.config.Namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{labelProcedure: dnsLabel(commandName)}).String(),
	})
	if err != nil {
		logger.Log().Error("Failed to list Kubernetes pods for attach", zap.String("namespace", b.config.Namespace), zap.String("command", commandName), zap.Error(err))
		return err
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			logger.Log().Debug("Attaching to Kubernetes pod", zap.String("namespace", b.config.Namespace), zap.String("pod", pod.Name), zap.String("command", commandName), zap.Int("bytes", len(data)))
			return b.attachToPod(context.Background(), b.config.Namespace, pod.Name, data)
		}
	}
	logger.Log().Warn("No running Kubernetes pod found for attach", zap.String("namespace", b.config.Namespace), zap.String("command", commandName), zap.Int("pods", len(pods.Items)))
	return fmt.Errorf("no running pod found for console %s", commandName)
}

func (b *Backend) attachToProcedure(root string, procedureName string, data string) error {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot attach to Kubernetes procedure for invalid root", zap.String("root", root), zap.String("procedure", procedureName), zap.Error(err))
		return err
	}
	selector := baseLabels(pvc)
	selector[labelProcedure] = dnsLabel(procedureName)
	podName, err := b.waitForPodBySelector(context.Background(), namespace, labels.SelectorFromSet(selector).String())
	if err != nil {
		logger.Log().Error("Failed to find Kubernetes procedure pod for attach", zap.String("namespace", namespace), zap.String("procedure", procedureName), zap.Any("selector", selector), zap.Error(err))
		return err
	}
	logger.Log().Debug("Attaching to Kubernetes procedure pod", zap.String("namespace", namespace), zap.String("pod", podName), zap.String("procedure", procedureName), zap.Int("bytes", len(data)))
	return b.attachToPod(context.Background(), namespace, podName, data)
}

func (b *Backend) attachToPod(ctx context.Context, namespace string, podName string, data string) error {
	logger.Log().Debug("Opening Kubernetes pod attach stream", zap.String("namespace", namespace), zap.String("pod", podName), zap.Int("bytes", len(data)))
	req := b.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
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
		logger.Log().Error("Failed to create Kubernetes pod attach executor", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
		return err
	}
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin: strings.NewReader(data),
	}); err != nil {
		logger.Log().Error("Kubernetes pod attach stream failed", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
		return err
	}
	logger.Log().Debug("Kubernetes pod attach stream completed", zap.String("namespace", namespace), zap.String("pod", podName))
	return nil
}

func (b *Backend) Signal(_ string, target string, signal string, root string) error {
	if target == "" {
		logger.Log().Warn("Ignoring Kubernetes signal with empty target", zap.String("root", root), zap.String("signal", signal))
		return nil
	}
	logger.Log().Info("Sending Kubernetes runtime signal", zap.String("root", root), zap.String("target", target), zap.String("signal", signal))
	switch signal {
	case "", "SIGTERM", "TERM":
		propagation := metav1.DeletePropagationBackground
		return b.deleteRuntimeWorkload(context.Background(), root, target, metav1.DeleteOptions{PropagationPolicy: &propagation})
	case "SIGKILL", "KILL":
		grace := int64(0)
		propagation := metav1.DeletePropagationBackground
		return b.deleteRuntimeWorkload(context.Background(), root, target, metav1.DeleteOptions{GracePeriodSeconds: &grace, PropagationPolicy: &propagation})
	default:
		logger.Log().Error("Unsupported Kubernetes signal", zap.String("root", root), zap.String("target", target), zap.String("signal", signal))
		return fmt.Errorf("kubernetes signal %s is unsupported without pod exec", signal)
	}
}

func (b *Backend) deleteRuntimeWorkload(ctx context.Context, root string, target string, options metav1.DeleteOptions) error {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot delete Kubernetes runtime workload for invalid root", zap.String("root", root), zap.String("target", target), zap.Error(err))
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID:  dnsLabel(pvc),
		labelProcedure: dnsLabel(target),
	}).String()
	logger.Log().Info("Deleting Kubernetes runtime workload", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("target", target), zap.String("selector", selector))
	jobs, jobErr := b.client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if jobErr == nil {
		logger.Log().Debug("Deleting Kubernetes workload jobs", zap.String("namespace", namespace), zap.String("target", target), zap.Int("jobs", len(jobs.Items)))
		for _, job := range jobs.Items {
			if err := b.client.BatchV1().Jobs(namespace).Delete(ctx, job.Name, options); err != nil && !apierrors.IsNotFound(err) {
				logger.Log().Error("Failed to delete Kubernetes workload job", zap.String("namespace", namespace), zap.String("job", job.Name), zap.String("target", target), zap.Error(err))
				jobErr = err
				break
			}
			logger.Log().Debug("Deleted Kubernetes workload job", zap.String("namespace", namespace), zap.String("job", job.Name), zap.String("target", target))
		}
	} else {
		logger.Log().Error("Failed to list Kubernetes workload jobs", zap.String("namespace", namespace), zap.String("target", target), zap.Error(jobErr))
	}
	statefulSets, statefulSetErr := b.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if statefulSetErr == nil {
		logger.Log().Debug("Deleting Kubernetes workload StatefulSets", zap.String("namespace", namespace), zap.String("target", target), zap.Int("statefulsets", len(statefulSets.Items)))
		for _, statefulSet := range statefulSets.Items {
			if err := b.client.AppsV1().StatefulSets(namespace).Delete(ctx, statefulSet.Name, options); err != nil && !apierrors.IsNotFound(err) {
				logger.Log().Error("Failed to delete Kubernetes workload StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.String("target", target), zap.Error(err))
				statefulSetErr = err
				break
			}
			logger.Log().Debug("Deleted Kubernetes workload StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.String("target", target))
		}
	} else {
		logger.Log().Error("Failed to list Kubernetes workload StatefulSets", zap.String("namespace", namespace), zap.String("target", target), zap.Error(statefulSetErr))
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
	namespace, pvc, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot delete Kubernetes runtime pods for invalid root", zap.String("root", root), zap.String("target", target), zap.Error(err))
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID:  dnsLabel(pvc),
		labelProcedure: dnsLabel(target),
	}).String()
	pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		logger.Log().Error("Failed to list Kubernetes runtime pods", zap.String("namespace", namespace), zap.String("target", target), zap.String("selector", selector), zap.Error(err))
		return err
	}
	logger.Log().Debug("Deleting Kubernetes runtime pods", zap.String("namespace", namespace), zap.String("target", target), zap.String("selector", selector), zap.Int("pods", len(pods.Items)))
	for _, pod := range pods.Items {
		if err := b.client.CoreV1().Pods(namespace).Delete(ctx, pod.Name, options); err != nil && !apierrors.IsNotFound(err) {
			logger.Log().Error("Failed to delete Kubernetes runtime pod", zap.String("namespace", namespace), zap.String("pod", pod.Name), zap.String("target", target), zap.Error(err))
			return err
		}
		logger.Log().Debug("Deleted Kubernetes runtime pod", zap.String("namespace", namespace), zap.String("pod", pod.Name), zap.String("target", target))
	}
	return nil
}

func (b *Backend) deleteRuntimeJobs(ctx context.Context, root string, options metav1.DeleteOptions) error {
	namespace, _, err := parseRef(root)
	if err != nil {
		return err
	}
	return b.deleteRuntimeObjects(ctx, root, func(name string) error {
		err := b.client.BatchV1().Jobs(namespace).Delete(ctx, name, options)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}, "jobs")
}

func (b *Backend) deleteRuntimeStatefulSets(ctx context.Context, root string, options metav1.DeleteOptions) error {
	namespace, _, err := parseRef(root)
	if err != nil {
		return err
	}
	return b.deleteRuntimeObjects(ctx, root, func(name string) error {
		err := b.client.AppsV1().StatefulSets(namespace).Delete(ctx, name, options)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}, "statefulsets")
}

func (b *Backend) deleteRuntimeServices(ctx context.Context, root string, options metav1.DeleteOptions) error {
	namespace, _, err := parseRef(root)
	if err != nil {
		return err
	}
	return b.deleteRuntimeObjects(ctx, root, func(name string) error {
		err := b.client.CoreV1().Services(namespace).Delete(ctx, name, options)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}, "services")
}

func (b *Backend) deleteRuntimeObjects(ctx context.Context, root string, deleteOne func(name string) error, kind string) error {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Cannot delete Kubernetes runtime objects for invalid root", zap.String("root", root), zap.String("kind", kind), zap.Error(err))
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID: dnsLabel(pvc),
	}).String()
	logger.Log().Debug("Deleting Kubernetes runtime objects", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("kind", kind), zap.String("selector", selector))
	switch kind {
	case "jobs":
		items, err := b.client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			logger.Log().Error("Failed to list Kubernetes runtime jobs", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Error(err))
			return err
		}
		logger.Log().Debug("Listed Kubernetes runtime jobs", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Int("jobs", len(items.Items)))
		for _, item := range items.Items {
			if err := deleteOne(item.Name); err != nil {
				logger.Log().Error("Failed to delete Kubernetes runtime job", zap.String("namespace", namespace), zap.String("job", item.Name), zap.Error(err))
				return err
			}
			logger.Log().Debug("Deleted Kubernetes runtime job", zap.String("namespace", namespace), zap.String("job", item.Name))
		}
	case "statefulsets":
		items, err := b.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			logger.Log().Error("Failed to list Kubernetes runtime StatefulSets", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Error(err))
			return err
		}
		logger.Log().Debug("Listed Kubernetes runtime StatefulSets", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Int("statefulsets", len(items.Items)))
		for _, item := range items.Items {
			if err := deleteOne(item.Name); err != nil {
				logger.Log().Error("Failed to delete Kubernetes runtime StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", item.Name), zap.Error(err))
				return err
			}
			logger.Log().Debug("Deleted Kubernetes runtime StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", item.Name))
		}
	case "services":
		items, err := b.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			logger.Log().Error("Failed to list Kubernetes runtime Services", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Error(err))
			return err
		}
		logger.Log().Debug("Listed Kubernetes runtime Services", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.Int("services", len(items.Items)))
		for _, item := range items.Items {
			if err := deleteOne(item.Name); err != nil {
				logger.Log().Error("Failed to delete Kubernetes runtime Service", zap.String("namespace", namespace), zap.String("service", item.Name), zap.Error(err))
				return err
			}
			logger.Log().Debug("Deleted Kubernetes runtime Service", zap.String("namespace", namespace), zap.String("service", item.Name))
		}
	}
	return nil
}

func (b *Backend) deleteRuntimePodsByScroll(ctx context.Context, root string, options metav1.DeleteOptions) error {
	namespace, pvc, err := parseRef(root)
	if err != nil {
		return err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID: dnsLabel(pvc),
	}).String()
	pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		logger.Log().Error("Failed to list Kubernetes runtime pods by scroll", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("selector", selector), zap.Error(err))
		return err
	}
	logger.Log().Debug("Deleting Kubernetes runtime pods by scroll", zap.String("namespace", namespace), zap.String("pvc", pvc), zap.String("selector", selector), zap.Int("pods", len(pods.Items)))
	for _, pod := range pods.Items {
		if err := b.client.CoreV1().Pods(namespace).Delete(ctx, pod.Name, options); err != nil && !apierrors.IsNotFound(err) {
			logger.Log().Error("Failed to delete Kubernetes runtime pod by scroll", zap.String("namespace", namespace), zap.String("pod", pod.Name), zap.Error(err))
			return err
		}
		logger.Log().Debug("Deleted Kubernetes runtime pod by scroll", zap.String("namespace", namespace), zap.String("pod", pod.Name))
	}
	return nil
}

func (b *Backend) ensurePVC(ctx context.Context, namespace string, name string) error {
	pvc := pvcSpec(namespace, name, b.config.StorageClass)
	logger.Log().Debug("Ensuring Kubernetes PVC", zap.String("namespace", namespace), zap.String("pvc", name), zap.String("storage_class", b.config.StorageClass))
	_, err := b.client.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		logger.Log().Debug("Kubernetes PVC already exists", zap.String("namespace", namespace), zap.String("pvc", name))
		return nil
	}
	if err != nil {
		logger.Log().Error("Failed to create Kubernetes PVC", zap.String("namespace", namespace), zap.String("pvc", name), zap.Error(err))
		return err
	}
	logger.Log().Info("Created Kubernetes PVC", zap.String("namespace", namespace), zap.String("pvc", name))
	return err
}

func (b *Backend) createRegistryConfigSecret(ctx context.Context, namespace string, seed string, credentials []domain.RegistryCredential) (string, func(), error) {
	if len(credentials) == 0 {
		logger.Log().Debug("No registry credentials supplied; skipping Kubernetes registry config secret", zap.String("namespace", namespace))
		return "", func() {}, nil
	}
	data, err := json.Marshal(struct {
		Registries []domain.RegistryCredential `json:"registries"`
	}{Registries: credentials})
	if err != nil {
		logger.Log().Error("Failed to marshal registry credentials for Kubernetes secret", zap.String("namespace", namespace), zap.Int("registries", len(credentials)), zap.Error(err))
		return "", nil, err
	}
	name := dnsLabel("druid-registry-" + shortHash(fmt.Sprintf("%s-%d", seed, time.Now().UnixNano())))
	logger.Log().Debug("Creating Kubernetes registry config secret", zap.String("namespace", namespace), zap.String("secret", name), zap.Int("registries", len(credentials)))
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
		logger.Log().Error("Failed to create Kubernetes registry config secret", zap.String("namespace", namespace), zap.String("secret", name), zap.Error(err))
		return "", nil, err
	}
	logger.Log().Info("Created Kubernetes registry config secret", zap.String("namespace", namespace), zap.String("secret", name), zap.Int("registries", len(credentials)))
	cleanup := func() {
		deleteCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := b.client.CoreV1().Secrets(namespace).Delete(deleteCtx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			logger.Log().Warn("Failed to delete Kubernetes registry config secret", zap.String("namespace", namespace), zap.String("secret", name), zap.Error(err))
			return
		}
		logger.Log().Debug("Deleted Kubernetes registry config secret", zap.String("namespace", namespace), zap.String("secret", name))
	}
	return name, cleanup, nil
}

func (b *Backend) runHelperJob(ctx context.Context, job *batchv1.Job) error {
	_, err := b.runJobAndLogs(ctx, job)
	if err != nil {
		logger.Log().Error("Kubernetes helper job failed", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(err))
	}
	return err
}

func (b *Backend) runJobAndLogs(ctx context.Context, job *batchv1.Job) ([]byte, error) {
	if b.jobLogRunner != nil {
		logger.Log().Debug("Running Kubernetes job through test log runner", zap.String("namespace", job.Namespace), zap.String("job", job.Name))
		return b.jobLogRunner(ctx, job)
	}
	logger.Log().Info("Starting Kubernetes helper job", zap.String("namespace", job.Namespace), zap.String("job", job.Name))
	logger.Log().Debug("Kubernetes helper job details", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.String("service_account", job.Spec.Template.Spec.ServiceAccountName), zap.Int("containers", len(job.Spec.Template.Spec.Containers)), zap.Int("init_containers", len(job.Spec.Template.Spec.InitContainers)))
	createdJob, err := b.createFreshJob(ctx, job)
	if err != nil {
		logger.Log().Error("Failed to create Kubernetes helper job", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(err))
		return nil, err
	}
	jobName := createdJob.Name
	podName, err := b.waitForJobPod(ctx, job.Namespace, jobName, string(createdJob.UID))
	if err != nil {
		logger.Log().Error("Failed to find Kubernetes helper job pod", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.String("uid", string(createdJob.UID)), zap.Error(err))
		return nil, err
	}
	logger.Log().Debug("Kubernetes helper job pod found", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.String("pod", podName))
	exitCode, waitErr := b.waitForJob(ctx, job.Namespace, jobName)
	logs, logErr := b.podLogs(ctx, job.Namespace, podName)
	if logErr != nil {
		logger.Log().Warn("Failed to collect Kubernetes helper job logs", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.String("pod", podName), zap.Error(logErr))
	} else {
		logger.Log().Debug("Collected Kubernetes helper job logs", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.String("pod", podName), zap.Int("bytes", len(logs)))
	}
	if exitCode != nil && *exitCode == 0 {
		b.deleteFinishedJob(context.Background(), job.Namespace, jobName)
	} else if exitCode != nil {
		logger.Log().Warn("Keeping failed Kubernetes helper job for debugging", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
	}
	if logErr != nil && waitErr == nil {
		waitErr = logErr
	}
	if waitErr != nil {
		logger.Log().Error("Kubernetes helper job wait failed", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.Any("exit_code", exitCode), zap.Error(waitErr))
		return logs, waitErr
	}
	if exitCode != nil && *exitCode != 0 {
		logger.Log().Error("Kubernetes helper job exited non-zero", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
		return logs, fmt.Errorf("job %s exited with code %d", jobName, *exitCode)
	}
	if exitCode != nil {
		logger.Log().Info("Kubernetes helper job completed", zap.String("namespace", job.Namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
	}
	return logs, nil
}

func (b *Backend) createFreshJob(ctx context.Context, job *batchv1.Job) (*batchv1.Job, error) {
	propagation := metav1.DeletePropagationBackground
	deleteCtx, cancelDelete := context.WithTimeout(ctx, 30*time.Second)
	defer cancelDelete()
	existing, err := b.client.BatchV1().Jobs(job.Namespace).Get(deleteCtx, job.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Log().Error("Failed to check Kubernetes job before create", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(err))
		return nil, err
	}
	if existing != nil && kubernetesJobFailed(existing) {
		original := job.Name
		job = job.DeepCopy()
		job.Name = dnsLabel(fmt.Sprintf("%s-%s", original, shortHash(fmt.Sprintf("%s-%d", original, time.Now().UnixNano()))))
		logger.Log().Warn("Retaining failed Kubernetes job and creating retry job", zap.String("namespace", job.Namespace), zap.String("failed_job", original), zap.String("retry_job", job.Name))
	}
	logger.Log().Debug("Deleting stale Kubernetes job before create", zap.String("namespace", job.Namespace), zap.String("job", job.Name))
	if err := b.client.BatchV1().Jobs(job.Namespace).Delete(deleteCtx, job.Name, metav1.DeleteOptions{PropagationPolicy: &propagation}); err != nil && !apierrors.IsNotFound(err) {
		logger.Log().Error("Failed to delete stale Kubernetes job before create", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(err))
		return nil, err
	}
	for {
		_, err := b.client.BatchV1().Jobs(job.Namespace).Get(deleteCtx, job.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			break
		}
		if err != nil {
			logger.Log().Error("Failed to check stale Kubernetes job before create", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(err))
			return nil, err
		}
		logger.Log().Debug("Waiting for stale Kubernetes job deletion", zap.String("namespace", job.Namespace), zap.String("job", job.Name))
		select {
		case <-deleteCtx.Done():
			logger.Log().Error("Timed out waiting for stale Kubernetes job deletion", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(deleteCtx.Err()))
			return nil, deleteCtx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	createdJob, err := b.client.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		logger.Log().Error("Failed to create Kubernetes job", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(err))
		return nil, err
	}
	logger.Log().Info("Created Kubernetes job", zap.String("namespace", job.Namespace), zap.String("job", createdJob.Name), zap.String("uid", string(createdJob.UID)))
	return createdJob, nil
}

func (b *Backend) deleteFinishedJob(ctx context.Context, namespace string, name string) {
	propagation := metav1.DeletePropagationBackground
	deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	logger.Log().Debug("Deleting finished Kubernetes job", zap.String("namespace", namespace), zap.String("job", name))
	err := b.client.BatchV1().Jobs(namespace).Delete(deleteCtx, name, metav1.DeleteOptions{PropagationPolicy: &propagation})
	if apierrors.IsNotFound(err) {
		logger.Log().Debug("Finished Kubernetes job was already absent", zap.String("namespace", namespace), zap.String("job", name))
		return
	}
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Log().Warn("Failed to delete finished Kubernetes job", zap.String("namespace", namespace), zap.String("job", name), zap.Error(err))
		return
	}
	logger.Log().Info("Deleted finished Kubernetes job", zap.String("namespace", namespace), zap.String("job", name))
}

func (b *Backend) waitForJobPod(ctx context.Context, namespace string, jobName string, controllerUID string) (string, error) {
	matchLabels := labels.Set{"job-name": jobName}
	if controllerUID != "" {
		matchLabels["controller-uid"] = controllerUID
	}
	selector := labels.SelectorFromSet(matchLabels).String()
	logger.Log().Debug("Waiting for Kubernetes job pod", zap.String("namespace", namespace), zap.String("job", jobName), zap.String("selector", selector), zap.String("controller_uid", controllerUID))
	return b.waitForPodBySelector(ctx, namespace, selector)
}

func (b *Backend) waitForPodBySelector(ctx context.Context, namespace string, selector string) (string, error) {
	deadline := time.Now().Add(2 * time.Minute)
	backoff := newCappedBackoff(podPollInitial, podPollMax)
	for {
		pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			logger.Log().Error("Failed to list Kubernetes pods while waiting", zap.String("namespace", namespace), zap.String("selector", selector), zap.Error(err))
			return "", err
		}
		if len(pods.Items) > 0 {
			logger.Log().Debug("Kubernetes pod matched selector", zap.String("namespace", namespace), zap.String("selector", selector), zap.String("pod", pods.Items[0].Name), zap.Int("matches", len(pods.Items)))
			return pods.Items[0].Name, nil
		}
		if time.Now().After(deadline) {
			logger.Log().Error("Timed out waiting for Kubernetes pod", zap.String("namespace", namespace), zap.String("selector", selector))
			return "", fmt.Errorf("timed out waiting for pod matching selector %s", selector)
		}
		sleep := backoff.Next()
		logger.Log().Debug("No Kubernetes pod matched yet", zap.String("namespace", namespace), zap.String("selector", selector), zap.Duration("sleep", sleep), zap.Time("deadline", deadline))
		if err := sleepUntilNextPoll(ctx, deadline, sleep); err != nil {
			logger.Log().Warn("Stopped waiting for Kubernetes pod", zap.String("namespace", namespace), zap.String("selector", selector), zap.Error(err))
			return "", err
		}
	}
}

func (b *Backend) waitForStatefulSet(ctx context.Context, namespace string, name string) error {
	deadline := time.Now().Add(5 * time.Minute)
	backoff := newCappedBackoff(statefulSetPollInitial, statefulSetPollMax)
	for {
		statefulSet, err := b.client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			logger.Log().Error("Failed to get Kubernetes StatefulSet while waiting", zap.String("namespace", namespace), zap.String("statefulset", name), zap.Error(err))
			return err
		}
		wanted := int32(1)
		if statefulSet.Spec.Replicas != nil {
			wanted = *statefulSet.Spec.Replicas
		}
		if statefulSet.Status.ReadyReplicas >= wanted {
			logger.Log().Debug("Kubernetes StatefulSet ready", zap.String("namespace", namespace), zap.String("statefulset", name), zap.Int32("ready", statefulSet.Status.ReadyReplicas), zap.Int32("wanted", wanted))
			return nil
		}
		if time.Now().After(deadline) {
			logger.Log().Error("Timed out waiting for Kubernetes StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", name), zap.Int32("ready", statefulSet.Status.ReadyReplicas), zap.Int32("wanted", wanted))
			return fmt.Errorf("timed out waiting for StatefulSet %s to become ready", name)
		}
		sleep := backoff.Next()
		logger.Log().Debug("Kubernetes StatefulSet not ready yet", zap.String("namespace", namespace), zap.String("statefulset", name), zap.Int32("ready", statefulSet.Status.ReadyReplicas), zap.Int32("wanted", wanted), zap.Duration("sleep", sleep), zap.Time("deadline", deadline))
		if err := sleepUntilNextPoll(ctx, deadline, sleep); err != nil {
			logger.Log().Warn("Stopped waiting for Kubernetes StatefulSet", zap.String("namespace", namespace), zap.String("statefulset", name), zap.Error(err))
			return err
		}
	}
}

func (b *Backend) waitForJob(ctx context.Context, namespace string, jobName string) (*int, error) {
	startedAt := time.Now()
	deadline := time.Now().Add(24 * time.Hour)
	for {
		job, err := b.client.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			logger.Log().Error("Failed to get Kubernetes job while waiting", zap.String("namespace", namespace), zap.String("job", jobName), zap.Error(err))
			return nil, err
		}
		if job.Status.Succeeded > 0 {
			exitCode := 0
			logger.Log().Debug("Kubernetes job succeeded", zap.String("namespace", namespace), zap.String("job", jobName), zap.Int32("succeeded", job.Status.Succeeded), zap.Int32("failed", job.Status.Failed), zap.Int32("active", job.Status.Active))
			return &exitCode, nil
		}
		if kubernetesJobFailed(job) {
			exitCode := b.lastExitCode(ctx, namespace, jobName)
			logger.Log().Error("Kubernetes job failed", zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", exitCode), zap.Int32("succeeded", job.Status.Succeeded), zap.Int32("failed", job.Status.Failed), zap.Int32("active", job.Status.Active))
			return &exitCode, fmt.Errorf("job %s failed", jobName)
		}
		if time.Now().After(deadline) {
			logger.Log().Error("Timed out waiting for Kubernetes job", zap.String("namespace", namespace), zap.String("job", jobName), zap.Int32("succeeded", job.Status.Succeeded), zap.Int32("failed", job.Status.Failed), zap.Int32("active", job.Status.Active))
			return nil, fmt.Errorf("timed out waiting for job %s", jobName)
		}
		sleep := jobPollInterval(time.Since(startedAt))
		logger.Log().Debug("Kubernetes job still running", zap.String("namespace", namespace), zap.String("job", jobName), zap.Int32("succeeded", job.Status.Succeeded), zap.Int32("failed", job.Status.Failed), zap.Int32("active", job.Status.Active), zap.Duration("sleep", sleep), zap.Time("deadline", deadline))
		if err := sleepUntilNextPoll(ctx, deadline, sleep); err != nil {
			logger.Log().Warn("Stopped waiting for Kubernetes job", zap.String("namespace", namespace), zap.String("job", jobName), zap.Error(err))
			return nil, err
		}
	}
}

func kubernetesJobFailed(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (b *Backend) lastExitCode(ctx context.Context, namespace string, jobName string) int {
	selector := labels.SelectorFromSet(labels.Set{"job-name": jobName}).String()
	pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil || len(pods.Items) == 0 {
		podCount := 0
		if pods != nil {
			podCount = len(pods.Items)
		}
		logger.Log().Warn("Could not read Kubernetes job exit code; defaulting to 1", zap.String("namespace", namespace), zap.String("job", jobName), zap.String("selector", selector), zap.Int("pods", podCount), zap.Error(err))
		return 1
	}
	for _, status := range pods.Items[0].Status.ContainerStatuses {
		if status.State.Terminated != nil {
			logger.Log().Debug("Read Kubernetes job container exit code", zap.String("namespace", namespace), zap.String("job", jobName), zap.String("pod", pods.Items[0].Name), zap.String("container", status.Name), zap.Int32("exit_code", status.State.Terminated.ExitCode))
			return int(status.State.Terminated.ExitCode)
		}
	}
	logger.Log().Warn("Kubernetes job pod had no terminated container status; defaulting to 1", zap.String("namespace", namespace), zap.String("job", jobName), zap.String("pod", pods.Items[0].Name))
	return 1
}

func (b *Backend) podLogs(ctx context.Context, namespace string, podName string) ([]byte, error) {
	logger.Log().Debug("Reading Kubernetes pod logs", zap.String("namespace", namespace), zap.String("pod", podName))
	req := b.client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{})
	stream, err := req.Stream(ctx)
	if err != nil {
		logger.Log().Warn("Failed to open Kubernetes pod log stream", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
		return nil, err
	}
	defer stream.Close()
	logs, err := io.ReadAll(stream)
	if err != nil {
		logger.Log().Warn("Failed to read Kubernetes pod logs", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
		return logs, err
	}
	logger.Log().Debug("Read Kubernetes pod logs", zap.String("namespace", namespace), zap.String("pod", podName), zap.Int("bytes", len(logs)))
	return logs, nil
}

func (b *Backend) streamPodLogs(ctx context.Context, namespace string, podName string, output chan<- string) {
	defer close(output)
	var stream io.ReadCloser
	deadline := time.Now().Add(30 * time.Second)
	logger.Log().Debug("Opening Kubernetes follow log stream", zap.String("namespace", namespace), zap.String("pod", podName))
	for {
		req := b.client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{Follow: true})
		var err error
		stream, err = req.Stream(ctx)
		if err == nil {
			logger.Log().Debug("Kubernetes follow log stream opened", zap.String("namespace", namespace), zap.String("pod", podName))
			break
		}
		if !strings.Contains(err.Error(), "ContainerCreating") &&
			!strings.Contains(err.Error(), "PodInitializing") &&
			!strings.Contains(err.Error(), "not available") {
			logger.Log().Warn("Failed to stream Kubernetes pod logs", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
			output <- fmt.Sprintf("failed to stream pod logs: %v", err)
			return
		}
		if time.Now().After(deadline) {
			logger.Log().Warn("Timed out opening Kubernetes pod log stream", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
			output <- fmt.Sprintf("failed to stream pod logs: %v", err)
			return
		}
		logger.Log().Debug("Kubernetes pod logs not ready yet", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
		select {
		case <-ctx.Done():
			logger.Log().Warn("Context cancelled while opening Kubernetes pod logs", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(ctx.Err()))
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
	if err := scanner.Err(); err != nil {
		logger.Log().Warn("Kubernetes pod log stream ended with scanner error", zap.String("namespace", namespace), zap.String("pod", podName), zap.Error(err))
		return
	}
	logger.Log().Debug("Kubernetes pod log stream ended", zap.String("namespace", namespace), zap.String("pod", podName))
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
	if portUse[portName] > 1 {
		selector[labelCommand] = dnsLabel(commandName)
		return selector
	}
	selector[labelProcedure] = dnsLabel(procedureName)
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
