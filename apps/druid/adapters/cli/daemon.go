package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
var k8sUIS3AccessKey string
var k8sUIS3SecretKey string
var k8sUIS3SessionToken string
var k8sKubeconfig string
var k8sServiceAccountAudience string
var k8sOperatorServiceAccount string
var runtimeListen string
var runtimePublicListen string
var runtimeAllowUnauthenticatedPublic bool
var runtimeAllowUnauthenticatedManagement bool
var runtimeWorkerTimeout time.Duration
var runtimeWorkerCallbackListen string
var runtimeWorkerCallbackURL string
var runtimeAuthJWKSURL string
var runtimePublicJWKSURL string
var dockerWorkerImage string
var dockerStorage string
var dockerBindRoot string
var dockerVolumePrefix string
var dockerUIS3Bucket string
var dockerUIS3PublicBaseURL string
var dockerUIS3Region string
var dockerUIS3Endpoint string
var dockerUIS3Prefix string
var dockerUIS3AccessKey string
var dockerUIS3SecretKey string
var dockerUIS3SessionToken string

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
	DaemonCommand.Flags().BoolVar(&runtimeAllowUnauthenticatedPublic, "unsafe-allow-unauthenticated-public", false, "Allow unauthenticated public HTTP routes without --auth-jwks-url")
	DaemonCommand.Flags().BoolVar(&runtimeAllowUnauthenticatedManagement, "unsafe-allow-unauthenticated-management", false, "Allow unauthenticated management HTTP routes; local Docker development only")
	DaemonCommand.Flags().DurationVar(&runtimeWorkerTimeout, "worker-timeout", 20*time.Minute, "Maximum time for runtime materialization workers")
	DaemonCommand.Flags().StringVar(&runtimeWorkerCallbackListen, "worker-callback-listen", "", "Optional internal worker callback listen address, for example :8083")
	DaemonCommand.Flags().StringVar(&runtimeWorkerCallbackURL, "worker-callback-url", "", "URL workers use to call back to this daemon")
	DaemonCommand.Flags().StringVar(&runtimeAuthJWKSURL, "auth-jwks-url", "", "JWKS URL used to validate customer JWTs")
	DaemonCommand.Flags().StringVar(&runtimePublicJWKSURL, "public-jwks-url", "", "Public JWKS URL workers use to validate daemon runtime tokens")
	DaemonCommand.Flags().StringVar(&dockerWorkerImage, "docker-worker-image", "", "Docker image used for sibling worker containers (default: DRUID_DOCKER_WORKER_IMAGE)")
	DaemonCommand.Flags().StringVar(&dockerStorage, "docker-storage", "", "Docker runtime storage mode: volume or bind (default: DRUID_DOCKER_STORAGE or volume)")
	DaemonCommand.Flags().StringVar(&dockerBindRoot, "docker-bind-root", "", "Host root for Docker bind storage (default: DRUID_DOCKER_BIND_ROOT)")
	DaemonCommand.Flags().StringVar(&dockerVolumePrefix, "docker-volume-prefix", "", "Docker volume name prefix (default: DRUID_DOCKER_VOLUME_PREFIX or druid)")
	DaemonCommand.Flags().StringVar(&dockerUIS3Bucket, "docker-ui-s3-bucket", "", "S3 bucket for published Docker UI packages (default: DRUID_DOCKER_UI_S3_BUCKET)")
	DaemonCommand.Flags().StringVar(&dockerUIS3PublicBaseURL, "docker-ui-s3-public-base-url", "", "Public base URL for published Docker UI packages (default: DRUID_DOCKER_UI_S3_PUBLIC_BASE_URL)")
	DaemonCommand.Flags().StringVar(&dockerUIS3Region, "docker-ui-s3-region", "", "S3 region for published Docker UI packages (default: DRUID_DOCKER_UI_S3_REGION)")
	DaemonCommand.Flags().StringVar(&dockerUIS3Endpoint, "docker-ui-s3-endpoint", "", "Optional S3-compatible endpoint for published Docker UI packages (default: DRUID_DOCKER_UI_S3_ENDPOINT)")
	DaemonCommand.Flags().StringVar(&dockerUIS3Prefix, "docker-ui-s3-prefix", "", "Optional S3 key prefix for published Docker UI packages (default: DRUID_DOCKER_UI_S3_PREFIX)")
	DaemonCommand.Flags().StringVar(&dockerUIS3AccessKey, "docker-ui-s3-access-key", "", "S3 access key for published Docker UI packages (default: DRUID_DOCKER_UI_S3_ACCESS_KEY)")
	DaemonCommand.Flags().StringVar(&dockerUIS3SecretKey, "docker-ui-s3-secret-key", "", "S3 secret key for published Docker UI packages (default: DRUID_DOCKER_UI_S3_SECRET_KEY)")
	DaemonCommand.Flags().StringVar(&dockerUIS3SessionToken, "docker-ui-s3-session-token", "", "Optional S3 session token for published Docker UI packages (default: DRUID_DOCKER_UI_S3_SESSION_TOKEN)")
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
	DaemonCommand.Flags().StringVar(&k8sUIS3AccessKey, "k8s-ui-s3-access-key", "", "S3 access key for published UI packages (default: DRUID_K8S_UI_S3_ACCESS_KEY)")
	DaemonCommand.Flags().StringVar(&k8sUIS3SecretKey, "k8s-ui-s3-secret-key", "", "S3 secret key for published UI packages (default: DRUID_K8S_UI_S3_SECRET_KEY)")
	DaemonCommand.Flags().StringVar(&k8sUIS3SessionToken, "k8s-ui-s3-session-token", "", "Optional S3 session token for published UI packages (default: DRUID_K8S_UI_S3_SESSION_TOKEN)")
	DaemonCommand.Flags().StringVar(&k8sKubeconfig, "k8s-kubeconfig", "", "Kubernetes kubeconfig path for out-of-cluster runtime access (default: DRUID_K8S_KUBECONFIG, KUBECONFIG, or ~/.kube/config)")
	DaemonCommand.Flags().StringVar(&k8sServiceAccountAudience, "k8s-service-account-audience", "", "Audience required for Druid workload ServiceAccount tokens")
	DaemonCommand.Flags().StringVar(&k8sOperatorServiceAccount, "k8s-operator-service-account", "", "Allowed deployment-operator ServiceAccount as namespace/name")
}

func runRuntimeDaemon() error {
	loadRuntimeDaemonEnv()
	if err := validateRuntimeDaemonAuthConfig(); err != nil {
		return err
	}
	kubernetesConfig := runtimekubernetes.Config{
		Namespace:              k8sNamespace,
		StorageClass:           k8sStorageClass,
		PullImage:              k8sPullImage,
		RegistrySecret:         k8sRegistrySecret,
		Kubeconfig:             k8sKubeconfig,
		UIS3Bucket:             k8sUIS3Bucket,
		UIS3PublicBaseURL:      k8sUIS3PublicBaseURL,
		UIS3Region:             k8sUIS3Region,
		UIS3Endpoint:           k8sUIS3Endpoint,
		UIS3Prefix:             k8sUIS3Prefix,
		UIS3AccessKey:          k8sUIS3AccessKey,
		UIS3SecretKey:          k8sUIS3SecretKey,
		UIS3SessionToken:       k8sUIS3SessionToken,
		ServiceAccountAudience: k8sServiceAccountAudience,
		OperatorServiceAccount: k8sOperatorServiceAccount,
	}
	dockerConfig := runtimedocker.Config{WorkerImage: dockerWorkerImage, Storage: dockerStorage, BindRoot: dockerBindRoot, VolumePrefix: dockerVolumePrefix, UIS3Bucket: dockerUIS3Bucket, UIS3PublicBaseURL: dockerUIS3PublicBaseURL, UIS3Region: dockerUIS3Region, UIS3Endpoint: dockerUIS3Endpoint, UIS3Prefix: dockerUIS3Prefix, UIS3AccessKey: dockerUIS3AccessKey, UIS3SecretKey: dockerUIS3SecretKey, UIS3SessionToken: dockerUIS3SessionToken}
	logManager := services.NewLogManager()
	consoleService := services.NewConsoleManager(logManager)
	runtime, err := runtimebackend.NewRuntime(runtimeBackendName, consoleService, runtimeStateDir, runtimebackend.WithKubernetesConfig(kubernetesConfig), runtimebackend.WithDockerConfig(dockerConfig))
	if err != nil {
		return err
	}
	manager := services.NewRuntimeScrollManager(runtime.Store)
	supervisor, err := appservices.NewRuntimeSupervisor(runtime.Store, manager, runtime)
	if err != nil {
		return err
	}
	callbacks := appservices.NewWorkerCallbackManager()
	supervisor.SetWorkerTimeout(runtimeWorkerTimeout)
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
	if err := supervisor.Start(); err != nil {
		return err
	}

	authorizer, err := services.NewAuthorizer(buildJWKSURLs([]string{runtimeAuthJWKSURL}), "")
	if err != nil {
		return err
	}
	scrollHandler := runtimehandlers.NewScrollHandler(supervisor, consoleService, logManager, authorizer)
	scrollHandler.SetAllowUnauthenticatedPublic(runtimeAllowUnauthenticatedPublic)
	websocketHandler := runtimehandlers.NewWebsocketHandler(consoleService)
	websocketHandler.SetScrollHandler(scrollHandler)
	websocketHandler.SetAuthorizer(authorizer)
	websocketHandler.SetAllowUnauthenticatedPublic(runtimeAllowUnauthenticatedPublic)
	handlers := runtimehandlers.RouteHandlers{
		Server: runtimehandlers.NewRuntimeServer(
			runtimehandlers.NewHealthHandler(),
			scrollHandler,
		),
		Websocket: websocketHandler,
	}

	managementApp := fiber.New(fiber.Config{DisableStartupMessage: true, ErrorHandler: runtimehandlers.ErrorHandler})
	managementApp.Use(runtimehandlers.RequestLogger)
	workloadAuthenticator, _ := runtime.Backend.(ports.RuntimeWorkloadAuthenticator)
	managementApp.Use(workloadIdentityMiddleware(workloadAuthenticator, runtimeAllowUnauthenticatedManagement))
	runtimehandlers.RegisterManagementRoutes(managementApp, handlers)

	var publicApp *fiber.App
	if runtimePublicListen != "" {
		publicApp = fiber.New(fiber.Config{DisableStartupMessage: true, ErrorHandler: runtimehandlers.ErrorHandler})
		publicApp.Use(runtimehandlers.RequestLogger)
		runtimehandlers.RegisterPublicRoutes(publicApp, handlers)
	}
	var callbackApp *fiber.App
	if callbackListener != nil {
		callbackAllowUnsafe := workerCallbackAllowsUnsafeFallback(workloadAuthenticator, runtimeAllowUnauthenticatedManagement)
		callbackApp = fiber.New(fiber.Config{DisableStartupMessage: true, ErrorHandler: runtimehandlers.ErrorHandler})
		callbackApp.Use(runtimehandlers.RequestLogger)
		callbackApp.Use(workloadIdentityMiddleware(workloadAuthenticator, callbackAllowUnsafe))
		callbackapi.RegisterHandlers(callbackApp, runtimeCallbackHandler{callbacks: callbacks, allowUnauthenticated: callbackAllowUnsafe})
	}
	return listenRuntimeHTTP(managementApp, publicApp, callbackApp, callbackListener, runtime.Store.StateDir())
}

func workerCallbackAllowsUnsafeFallback(authenticator ports.RuntimeWorkloadAuthenticator, allowUnsafeManagement bool) bool {
	return allowUnsafeManagement && authenticator == nil
}

func loadRuntimeDaemonEnv() {
	if runtimeWorkerCallbackURL == "" {
		runtimeWorkerCallbackURL = os.Getenv("DRUID_WORKER_CALLBACK_URL")
	}
	if runtimeWorkerCallbackListen == "" {
		runtimeWorkerCallbackListen = os.Getenv("DRUID_WORKER_CALLBACK_LISTEN")
	}
	if runtimeAuthJWKSURL == "" {
		runtimeAuthJWKSURL = os.Getenv("DRUID_AUTH_JWKS_URL")
	}
	if runtimePublicJWKSURL == "" {
		runtimePublicJWKSURL = os.Getenv("DRUID_PUBLIC_JWKS_URL")
	}
	if !runtimeAllowUnauthenticatedPublic {
		runtimeAllowUnauthenticatedPublic = envBool("DRUID_UNSAFE_ALLOW_UNAUTHENTICATED_PUBLIC")
	}
	if !runtimeAllowUnauthenticatedManagement {
		runtimeAllowUnauthenticatedManagement = envBool("DRUID_UNSAFE_ALLOW_UNAUTHENTICATED_MANAGEMENT")
	}
	if runtimeWorkerTimeout == 0 {
		runtimeWorkerTimeout = 20 * time.Minute
	}
	if raw := strings.TrimSpace(os.Getenv("DRUID_WORKER_TIMEOUT")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			runtimeWorkerTimeout = parsed
		}
	}
}

func envBool(name string) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func validateRuntimeDaemonAuthConfig() error {
	if runtimePublicListen != "" && runtimeAuthJWKSURL == "" && !runtimeAllowUnauthenticatedPublic {
		return fmt.Errorf("public listener %s requires --auth-jwks-url or --unsafe-allow-unauthenticated-public", runtimePublicListen)
	}
	if runtimeListen != "" && runtimeBackendName != "kubernetes" && !runtimeAllowUnauthenticatedManagement {
		return fmt.Errorf("management listener %s requires Kubernetes workload identity or --unsafe-allow-unauthenticated-management for local Docker development", runtimeListen)
	}
	if runtimeListen != "" && runtimeBackendName == "kubernetes" && !runtimeAllowUnauthenticatedManagement && strings.TrimSpace(k8sOperatorServiceAccount) == "" {
		return fmt.Errorf("Kubernetes management listener requires --k8s-operator-service-account")
	}
	if runtimeWorkerTimeout <= 0 {
		return fmt.Errorf("worker timeout must be greater than zero")
	}
	return nil
}

func workloadIdentityMiddleware(authenticator ports.RuntimeWorkloadAuthenticator, allowUnsafe bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Path() == "/health" || c.Path() == "/api/v1/health" {
			return c.Next()
		}
		token := strings.TrimSpace(strings.TrimPrefix(c.Get("Authorization"), "Bearer "))
		if allowUnsafe && token == "" {
			runtimeID := strings.TrimSpace(c.Get("X-Druid-Runtime-ID"))
			c.Locals("druid-workload-identity", ports.RuntimeWorkloadIdentity{Kind: "unsafe", RuntimeID: runtimeID, PodUID: "unsafe-local"})
			return c.Next()
		}
		if authenticator == nil {
			return fiber.NewError(fiber.StatusUnauthorized, "workload identity authentication is unavailable")
		}
		identity, err := authenticator.AuthenticateWorkload(c.Context(), token)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, err.Error())
		}
		if identity.Kind == "operator" {
			c.Locals("druid-workload-identity", identity)
			return c.Next()
		}
		if identity.Kind == "worker" && strings.HasPrefix(c.Path(), "/internal/v1/workers/"+identity.RuntimeID+"/") {
			c.Locals("druid-workload-identity", identity)
			return c.Next()
		}
		return fiber.NewError(fiber.StatusForbidden, "workload identity is not authorized for this request")
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
