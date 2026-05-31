package kubernetes

import (
	"context"
	"fmt"
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type Backend struct {
	client         k8sclient.Interface
	restConfig     *rest.Config
	consoleManager ports.ConsoleManagerInterface
	config         Config
	statsReader    nodeStatsReader
	jobLogRunner   func(context.Context, *batchv1.Job) ([]byte, error)
	jobExitMu      sync.Mutex
	jobExits       map[string]recentJobExit
}

type recentJobExit struct {
	exitCode   int
	recordedAt time.Time
}

const (
	defaultKubernetesClientQPS   float32 = 50
	defaultKubernetesClientBurst int     = 100
)

func New(config Config, consoleManager ports.ConsoleManagerInterface) (*Backend, error) {
	config = config.WithDefaults()

	restConfig, namespace, source, _, err := runtimeRESTConfig(config)
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
		jobExits:       make(map[string]recentJobExit),
	}
	backend.statsReader = backend.readNodeStatsSummary
	if config.PullImage == "" {
		logger.Log().Warn("Kubernetes cluster materialization requires --k8s-pull-image or DRUID_K8S_PULL_IMAGE")
	}
	return backend, nil
}

func runtimeRESTConfig(config Config) (*rest.Config, string, string, bool, error) {
	restConfig, inClusterErr := rest.InClusterConfig()
	if inClusterErr == nil {
		tuneRESTConfig(restConfig)
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
	tuneRESTConfig(restConfig)
	return restConfig, namespace, source, false, nil
}

func tuneRESTConfig(config *rest.Config) {
	if config == nil {
		return
	}
	if config.QPS == 0 {
		config.QPS = defaultKubernetesClientQPS
	}
	if config.Burst == 0 {
		config.Burst = defaultKubernetesClientBurst
	}
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

func NewWithClient(config Config, consoleManager ports.ConsoleManagerInterface, client k8sclient.Interface) *Backend {
	config = config.WithDefaults()
	if config.Namespace == "" {
		config.Namespace = "default"
	}
	backend := &Backend{client: client, consoleManager: consoleManager, config: config, jobExits: make(map[string]recentJobExit)}
	backend.statsReader = backend.readNodeStatsSummary
	return backend
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
