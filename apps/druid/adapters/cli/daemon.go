package cli

import (
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	runtimehandlers "github.com/highcard-dev/daemon/apps/druid/adapters/http/handlers"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/callbackapi"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/core/services"
	runtimebackend "github.com/highcard-dev/daemon/internal/runtime"
	runtimedocker "github.com/highcard-dev/daemon/internal/runtime/docker"
	runtimekubernetes "github.com/highcard-dev/daemon/internal/runtime/kubernetes"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var runtimeSocket string
var k8sNamespace string
var k8sStorageClass string
var k8sPullImage string
var k8sRegistrySecret string
var hubbleRelayAddr string
var k8sKubeconfig string
var runtimeListen string
var runtimePublicListen string
var runtimeInternalToken string
var runtimeWorkerCallbackListen string
var runtimeWorkerCallbackURL string
var runtimeWorkerDaemonURL string
var runtimeAuthJWKSURL string
var runtimePublicJWKSURL string
var dockerWorkerImage string
var dockerStorage string
var dockerBindRoot string
var dockerVolumePrefix string

var DaemonCommand = &cobra.Command{
	Use:     "daemon",
	Aliases: []string{"serve"},
	Short:   "Run the multi-scroll runtime daemon",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRuntimeDaemon()
	},
}

func init() {
	RootCmd.AddCommand(DaemonCommand)
	DaemonCommand.Flags().StringVar(&runtimeSocket, "socket", utils.DefaultRuntimeSocketPath(), "Runtime daemon Unix socket path")
	DaemonCommand.Flags().StringVar(&runtimeListen, "listen", "", "Optional management HTTP listen address, for example :8081")
	DaemonCommand.Flags().StringVar(&runtimePublicListen, "public-listen", "", "Optional public dashboard HTTP listen address, for example :8082")
	DaemonCommand.Flags().StringVar(&runtimeInternalToken, "internal-token", "", "Optional bearer token required for management HTTP API requests")
	DaemonCommand.Flags().StringVar(&runtimeWorkerCallbackListen, "worker-callback-listen", "", "Optional internal worker callback listen address, for example :8083")
	DaemonCommand.Flags().StringVar(&runtimeWorkerCallbackURL, "worker-callback-url", "", "URL workers use to call back to this daemon")
	DaemonCommand.Flags().StringVar(&runtimeWorkerDaemonURL, "worker-daemon-url", "", "URL dev workers use for daemon management API calls")
	DaemonCommand.Flags().StringVar(&runtimeAuthJWKSURL, "auth-jwks-url", "", "JWKS URL used to validate customer JWTs")
	DaemonCommand.Flags().StringVar(&runtimePublicJWKSURL, "public-jwks-url", "", "Public JWKS URL workers use to validate daemon runtime tokens")
	DaemonCommand.Flags().StringVar(&dockerWorkerImage, "docker-worker-image", "", "Docker image used for sibling worker containers (default: DRUID_DOCKER_WORKER_IMAGE)")
	DaemonCommand.Flags().StringVar(&dockerStorage, "docker-storage", "", "Docker runtime storage mode: volume or bind (default: DRUID_DOCKER_STORAGE or volume)")
	DaemonCommand.Flags().StringVar(&dockerBindRoot, "docker-bind-root", "", "Host root for Docker bind storage (default: DRUID_DOCKER_BIND_ROOT)")
	DaemonCommand.Flags().StringVar(&dockerVolumePrefix, "docker-volume-prefix", "", "Docker volume name prefix (default: DRUID_DOCKER_VOLUME_PREFIX or druid)")
	DaemonCommand.Flags().StringVar(&runtimeStateDir, "state-dir", "", "Runtime state directory (default: ~/.druid/runtime)")
	DaemonCommand.Flags().StringVar(&runtimeBackendName, "runtime", "docker", "Default runtime backend. Valid values: docker, kubernetes")
	DaemonCommand.Flags().StringVar(&k8sNamespace, "k8s-namespace", "", "Kubernetes namespace for runtime resources (default: service account namespace or DRUID_K8S_NAMESPACE)")
	DaemonCommand.Flags().StringVar(&k8sStorageClass, "k8s-storage-class", "", "Kubernetes storage class for runtime PVCs (default: DRUID_K8S_STORAGE_CLASS)")
	DaemonCommand.Flags().StringVar(&k8sPullImage, "k8s-pull-image", "", "Kubernetes image used for OCI pull materialization Jobs (default: DRUID_K8S_PULL_IMAGE)")
	DaemonCommand.Flags().StringVar(&k8sRegistrySecret, "k8s-registry-secret", "", "Kubernetes imagePullSecret used by runtime Jobs (default: DRUID_K8S_REGISTRY_SECRET)")
	DaemonCommand.Flags().StringVar(&k8sKubeconfig, "k8s-kubeconfig", "", "Kubernetes kubeconfig path for out-of-cluster runtime access (default: DRUID_K8S_KUBECONFIG, KUBECONFIG, or ~/.kube/config)")
	DaemonCommand.Flags().StringVar(&hubbleRelayAddr, "hubble-relay-addr", "", "Hubble Relay gRPC address for Kubernetes port traffic (default: DRUID_HUBBLE_RELAY_ADDR or hubble-relay.kube-system.svc.cluster.local:80)")
}

func runRuntimeDaemon() error {
	kubernetesConfig := runtimekubernetes.Config{
		Namespace:       k8sNamespace,
		StorageClass:    k8sStorageClass,
		PullImage:       k8sPullImage,
		RegistrySecret:  k8sRegistrySecret,
		HubbleRelayAddr: hubbleRelayAddr,
		Kubeconfig:      k8sKubeconfig,
	}
	dockerConfig := runtimedocker.Config{WorkerImage: dockerWorkerImage, Storage: dockerStorage, BindRoot: dockerBindRoot, VolumePrefix: dockerVolumePrefix}
	logManager := services.NewLogManager()
	consoleService := services.NewConsoleManager(logManager)
	runtime, err := runtimebackend.NewRuntime(runtimeBackendName, consoleService, runtimeStateDir, runtimebackend.WithKubernetesConfig(kubernetesConfig), runtimebackend.WithDockerConfig(dockerConfig))
	if err != nil {
		return err
	}
	manager := services.NewRuntimeScrollManager(runtime.Store)
	supervisor := appservices.NewRuntimeSupervisor(runtime.Store, manager, runtime.Backend)
	callbacks := appservices.NewWorkerCallbackManager()
	loadRuntimeDaemonEnv()
	callbackConfig := ports.RuntimeWorkerCallbackConfig{
		Listen: runtimeWorkerCallbackListen,
		URL:    runtimeWorkerCallbackURL,
	}
	callbackBackend, _ := runtime.Backend.(ports.RuntimeWorkerCallbackBackend)
	if callbackBackend != nil {
		callbackConfig = callbackBackend.WorkerCallbackDefaults(callbackConfig)
	}
	callbackListener, err := openWorkerCallbackListener(callbackConfig.Listen)
	if err != nil {
		return err
	}
	if callbackListener != nil {
		callbackConfig.Listen = callbackListener.Addr().String()
		if callbackBackend != nil {
			callbackConfig, err = callbackBackend.WorkerCallbackAfterListen(callbackConfig)
			if err != nil {
				callbackListener.Close()
				return err
			}
		}
	}
	runtimeWorkerCallbackListen = callbackConfig.Listen
	runtimeWorkerCallbackURL = callbackConfig.URL
	supervisor.SetWorkerCallbacks(callbacks, runtimeWorkerCallbackURL)
	supervisor.SetDevWorkerConfig(runtimeWorkerDaemonURL, runtimeInternalToken, runtimeAuthJWKSURL, runtimePublicJWKSURL)
	if err := supervisor.Start(); err != nil {
		return err
	}

	authorizer, err := services.NewAuthorizer(runtimeAuthJWKSURL, "")
	if err != nil {
		return err
	}
	scrollHandler := runtimehandlers.NewScrollHandler(supervisor, consoleService, logManager, authorizer)
	websocketHandler := runtimehandlers.NewWebsocketHandler(consoleService)
	websocketHandler.SetScrollHandler(scrollHandler)
	websocketHandler.SetAuthorizer(authorizer)
	handlers := runtimehandlers.RouteHandlers{
		Server: runtimehandlers.NewRuntimeServer(
			runtimehandlers.NewHealthHandler(),
			scrollHandler,
		),
		Websocket: websocketHandler,
	}

	managementApp := fiber.New(fiber.Config{DisableStartupMessage: true, ErrorHandler: runtimehandlers.ErrorHandler})
	managementApp.Use(runtimehandlers.RequestLogger)
	if runtimeInternalToken != "" {
		managementApp.Use(func(c *fiber.Ctx) error {
			path := c.Path()
			if path == "/health" || path == "/api/v1/health" {
				return c.Next()
			}
			token := strings.TrimPrefix(c.Get("Authorization"), "Bearer ")
			if token == "" {
				token = c.Get("X-Druid-Internal-Token")
			}
			if token != runtimeInternalToken {
				return fiber.NewError(fiber.StatusUnauthorized, "invalid internal runtime token")
			}
			return c.Next()
		})
	}
	runtimehandlers.RegisterManagementRoutes(managementApp, handlers)

	var publicApp *fiber.App
	if runtimePublicListen != "" {
		publicApp = fiber.New(fiber.Config{DisableStartupMessage: true, ErrorHandler: runtimehandlers.ErrorHandler})
		publicApp.Use(runtimehandlers.RequestLogger)
		runtimehandlers.RegisterPublicRoutes(publicApp, handlers)
	}
	var callbackApp *fiber.App
	if callbackListener != nil {
		callbackApp = fiber.New(fiber.Config{DisableStartupMessage: true, ErrorHandler: runtimehandlers.ErrorHandler})
		callbackApp.Use(runtimehandlers.RequestLogger)
		callbackapi.RegisterHandlers(callbackApp, runtimeCallbackHandler{callbacks: callbacks})
	}
	return listenRuntimeHTTP(managementApp, publicApp, callbackApp, callbackListener, runtime.Store.StateDir())
}

func loadRuntimeDaemonEnv() {
	if runtimeWorkerCallbackURL == "" {
		runtimeWorkerCallbackURL = os.Getenv("DRUID_WORKER_CALLBACK_URL")
	}
	if runtimeWorkerCallbackListen == "" {
		runtimeWorkerCallbackListen = os.Getenv("DRUID_WORKER_CALLBACK_LISTEN")
	}
	if runtimeWorkerDaemonURL == "" {
		runtimeWorkerDaemonURL = os.Getenv("DRUID_WORKER_DAEMON_URL")
	}
	if runtimeAuthJWKSURL == "" {
		runtimeAuthJWKSURL = os.Getenv("DRUID_AUTH_JWKS_URL")
	}
	if runtimePublicJWKSURL == "" {
		runtimePublicJWKSURL = os.Getenv("DRUID_PUBLIC_JWKS_URL")
	}
	if runtimeInternalToken == "" {
		runtimeInternalToken = os.Getenv("DRUID_INTERNAL_TOKEN")
	}
}

func openWorkerCallbackListener(listen string) (net.Listener, error) {
	if listen == "" {
		return nil, nil
	}
	return net.Listen("tcp", listen)
}

func listenRuntimeHTTP(managementApp *fiber.App, publicApp *fiber.App, callbackApp *fiber.App, callbackListener net.Listener, stateDir string) error {
	errCh := make(chan error, 4)
	go func() {
		errCh <- listenRuntimeDaemon(managementApp, stateDir)
	}()
	if runtimeListen != "" {
		go func() {
			logger.Log().Info("Starting runtime management listener", zap.String("listen", runtimeListen), zap.String("stateDir", stateDir))
			errCh <- managementApp.Listen(runtimeListen)
		}()
	}
	if publicApp != nil {
		go func() {
			logger.Log().Info("Starting runtime public listener", zap.String("listen", runtimePublicListen), zap.String("stateDir", stateDir))
			errCh <- publicApp.Listen(runtimePublicListen)
		}()
	}
	if callbackApp != nil {
		go func() {
			logger.Log().Info("Starting runtime worker callback listener", zap.String("listen", runtimeWorkerCallbackListen), zap.String("url", runtimeWorkerCallbackURL), zap.String("stateDir", stateDir))
			errCh <- callbackApp.Listener(callbackListener)
		}()
	}
	return <-errCh
}

func listenRuntimeDaemon(app *fiber.App, stateDir string) error {
	if runtimeSocket == "" {
		runtimeSocket = utils.DefaultRuntimeSocketPath()
	}
	if err := os.MkdirAll(filepath.Dir(runtimeSocket), 0755); err != nil {
		return err
	}
	_ = os.Remove(runtimeSocket)
	listener, err := net.Listen("unix", runtimeSocket)
	if err != nil {
		return err
	}
	defer os.Remove(runtimeSocket)
	logger.Log().Info("Starting runtime daemon", zap.String("socket", runtimeSocket), zap.String("stateDir", stateDir))
	return app.Listener(listener)
}
