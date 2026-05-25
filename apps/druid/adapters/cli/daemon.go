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
var k8sUIS3Bucket string
var k8sUIS3PublicBaseURL string
var k8sUIS3Region string
var k8sUIS3Endpoint string
var k8sUIS3Prefix string
var k8sUIS3Secret string
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
var dockerUIBind string
var dockerUIPublicURL string

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
	DaemonCommand.Flags().StringVar(&dockerUIBind, "docker-ui-bind", "", "Docker UI package static server bind address (default: DRUID_DOCKER_UI_BIND or 127.0.0.1:8085)")
	DaemonCommand.Flags().StringVar(&dockerUIPublicURL, "docker-ui-public-url", "", "Public base URL for Docker UI packages (default: DRUID_DOCKER_UI_PUBLIC_URL or http://127.0.0.1:8085)")
	DaemonCommand.Flags().StringVar(&runtimeStateDir, "state-dir", "", "Runtime state directory (default: ~/.druid/runtime)")
	DaemonCommand.Flags().StringVar(&runtimeBackendName, "runtime", "docker", "Default runtime backend. Valid values: docker, kubernetes")
	DaemonCommand.Flags().StringVar(&k8sNamespace, "k8s-namespace", "", "Kubernetes namespace for runtime resources (default: service account namespace or DRUID_K8S_NAMESPACE)")
	DaemonCommand.Flags().StringVar(&k8sStorageClass, "k8s-storage-class", "", "Kubernetes storage class for runtime PVCs (default: DRUID_K8S_STORAGE_CLASS)")
	DaemonCommand.Flags().StringVar(&k8sPullImage, "k8s-pull-image", "", "Kubernetes image used for OCI pull materialization Jobs (default: DRUID_K8S_PULL_IMAGE)")
	DaemonCommand.Flags().StringVar(&k8sRegistrySecret, "k8s-registry-secret", "", "Kubernetes imagePullSecret used by runtime Jobs (default: DRUID_K8S_REGISTRY_SECRET)")
	DaemonCommand.Flags().StringVar(&k8sUIS3Bucket, "k8s-ui-s3-bucket", "", "S3 bucket for published UI packages (default: DRUID_K8S_UI_S3_BUCKET)")
	DaemonCommand.Flags().StringVar(&k8sUIS3PublicBaseURL, "k8s-ui-s3-public-base-url", "", "Public base URL for published UI packages (default: DRUID_K8S_UI_S3_PUBLIC_BASE_URL)")
	DaemonCommand.Flags().StringVar(&k8sUIS3Region, "k8s-ui-s3-region", "", "S3 region for published UI packages (default: DRUID_K8S_UI_S3_REGION)")
	DaemonCommand.Flags().StringVar(&k8sUIS3Endpoint, "k8s-ui-s3-endpoint", "", "Optional S3-compatible endpoint for UI packages (default: DRUID_K8S_UI_S3_ENDPOINT)")
	DaemonCommand.Flags().StringVar(&k8sUIS3Prefix, "k8s-ui-s3-prefix", "", "Optional S3 key prefix for UI packages (default: DRUID_K8S_UI_S3_PREFIX)")
	DaemonCommand.Flags().StringVar(&k8sUIS3Secret, "k8s-ui-s3-credentials-secret", "", "Kubernetes secret with AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY (default: DRUID_K8S_UI_S3_CREDENTIALS_SECRET)")
	DaemonCommand.Flags().StringVar(&k8sKubeconfig, "k8s-kubeconfig", "", "Kubernetes kubeconfig path for out-of-cluster runtime access (default: DRUID_K8S_KUBECONFIG, KUBECONFIG, or ~/.kube/config)")
	DaemonCommand.Flags().StringVar(&hubbleRelayAddr, "hubble-relay-addr", "", "Hubble Relay gRPC address for Kubernetes port traffic (default: DRUID_HUBBLE_RELAY_ADDR or hubble-relay.kube-system.svc.cluster.local:80)")
}

func runRuntimeDaemon() error {
	kubernetesConfig := runtimekubernetes.Config{
		Namespace:         k8sNamespace,
		StorageClass:      k8sStorageClass,
		PullImage:         k8sPullImage,
		RegistrySecret:    k8sRegistrySecret,
		HubbleRelayAddr:   hubbleRelayAddr,
		Kubeconfig:        k8sKubeconfig,
		UIS3Bucket:        k8sUIS3Bucket,
		UIS3PublicBaseURL: k8sUIS3PublicBaseURL,
		UIS3Region:        k8sUIS3Region,
		UIS3Endpoint:      k8sUIS3Endpoint,
		UIS3Prefix:        k8sUIS3Prefix,
		UIS3Secret:        k8sUIS3Secret,
	}
	dockerConfig := runtimedocker.Config{WorkerImage: dockerWorkerImage, Storage: dockerStorage, BindRoot: dockerBindRoot, VolumePrefix: dockerVolumePrefix, UIBind: dockerUIBind, UIPublicURL: dockerUIPublicURL}
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

	authorizer, err := services.NewAuthorizer(buildJWKSURLs([]string{runtimeAuthJWKSURL}), "")
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

func buildJWKSURLs(values []string) []string {
	urls := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))

	for _, url := range values {
		url = strings.TrimSpace(url)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}

	return urls
}
