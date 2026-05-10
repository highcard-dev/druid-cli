package cli

import (
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	runtimehandlers "github.com/highcard-dev/daemon/apps/druid/adapters/http/handlers"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/core/services"
	runtimebackend "github.com/highcard-dev/daemon/internal/runtime"
	runtimekubernetes "github.com/highcard-dev/daemon/internal/runtime/kubernetes"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var runtimeSocket string
var jwksUrls []string
var userId string
var k8sNamespace string
var k8sStorageClass string
var k8sPullImage string
var k8sRegistrySecret string
var hubbleRelayAddr string
var k8sKubeconfig string
var runtimeListen string
var runtimePublicListen string
var runtimeInternalToken string

var ServeCommand = &cobra.Command{
	Use:   "serve",
	Short: "Run the multi-scroll runtime daemon",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRuntimeDaemon()
	},
}

func init() {
	ServeCommand.Flags().StringVar(&runtimeSocket, "socket", utils.DefaultRuntimeSocketPath(), "Runtime daemon Unix socket path")
	ServeCommand.Flags().StringSliceVar(&jwksUrls, "jwks-server", nil, "JWKS servers to authenticate requests against. Can be comma-separated or set multiple times")
	ServeCommand.Flags().StringVarP(&userId, "user-id", "u", "", "Allowed user ID. When JWKS authentication is enabled, checks claims.sub of the JWT token")
	ServeCommand.Flags().StringVar(&runtimeListen, "listen", "", "Optional management HTTP listen address, for example :8081")
	ServeCommand.Flags().StringVar(&runtimePublicListen, "public-listen", "", "Optional public dashboard HTTP listen address, for example :8082")
	ServeCommand.Flags().StringVar(&runtimeInternalToken, "internal-token", "", "Optional bearer token required for management HTTP API requests")
	ServeCommand.Flags().StringVar(&runtimeStateDir, "state-dir", "", "Runtime state directory (default: ~/.druid/runtime)")
	ServeCommand.Flags().StringVar(&runtimeBackend, "runtime", "docker", "Default runtime backend. Valid values: docker, kubernetes")
	ServeCommand.Flags().StringVar(&k8sNamespace, "k8s-namespace", "", "Kubernetes namespace for runtime resources (default: service account namespace or DRUID_K8S_NAMESPACE)")
	ServeCommand.Flags().StringVar(&k8sStorageClass, "k8s-storage-class", "", "Kubernetes storage class for runtime PVCs (default: DRUID_K8S_STORAGE_CLASS)")
	ServeCommand.Flags().StringVar(&k8sPullImage, "k8s-pull-image", "", "Kubernetes image used for OCI pull materialization Jobs (default: DRUID_K8S_PULL_IMAGE)")
	ServeCommand.Flags().StringVar(&k8sRegistrySecret, "k8s-registry-secret", "", "Kubernetes imagePullSecret used by runtime Jobs (default: DRUID_K8S_REGISTRY_SECRET)")
	ServeCommand.Flags().StringVar(&k8sKubeconfig, "k8s-kubeconfig", "", "Kubernetes kubeconfig path for out-of-cluster runtime access (default: DRUID_K8S_KUBECONFIG, KUBECONFIG, or ~/.kube/config)")
	ServeCommand.Flags().StringVar(&hubbleRelayAddr, "hubble-relay-addr", "", "Hubble Relay gRPC address for Kubernetes port traffic (default: DRUID_HUBBLE_RELAY_ADDR or hubble-relay.kube-system.svc.cluster.local:80)")
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
	store, err := appservices.NewRuntimeStoreForBackend(runtimeStateDir, runtimeBackend, kubernetesConfig)
	if err != nil {
		return err
	}
	manager := services.NewRuntimeScrollManager(store)
	logManager := services.NewLogManager()
	consoleService := services.NewConsoleManager(logManager)
	authorizer, err := services.NewAuthorizer(buildJWKSURLs(jwksUrls), userId)
	if err != nil {
		return err
	}
	supervisor := appservices.NewRuntimeSupervisor(store, manager, consoleService, runtimeBackend, runtimebackend.WithKubernetesConfig(kubernetesConfig))
	if err := supervisor.Start(); err != nil {
		return err
	}

	if runtimeInternalToken == "" {
		runtimeInternalToken = os.Getenv("DRUID_INTERNAL_TOKEN")
	}
	handlers := runtimehandlers.RouteHandlers{
		Server: runtimehandlers.NewRuntimeServer(
			runtimehandlers.NewHealthHandler(),
			runtimehandlers.NewScrollHandler(supervisor, consoleService, logManager),
		),
		Websocket:  runtimehandlers.NewWebsocketHandler(consoleService),
		Authorizer: authorizer,
	}

	managementApp := fiber.New(fiber.Config{DisableStartupMessage: true})
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
		publicApp = fiber.New(fiber.Config{DisableStartupMessage: true})
		runtimehandlers.RegisterPublicRoutes(publicApp, handlers)
	}
	return listenRuntimeHTTP(managementApp, publicApp, store.StateDir())
}

func listenRuntimeHTTP(managementApp *fiber.App, publicApp *fiber.App, stateDir string) error {
	errCh := make(chan error, 2)
	if runtimeListen != "" {
		go func() {
			logger.Log().Info("Starting runtime management listener", zap.String("listen", runtimeListen), zap.String("stateDir", stateDir))
			errCh <- managementApp.Listen(runtimeListen)
		}()
	} else {
		go func() {
			errCh <- listenRuntimeDaemon(managementApp, stateDir)
		}()
	}
	if publicApp != nil {
		go func() {
			logger.Log().Info("Starting runtime public listener", zap.String("listen", runtimePublicListen), zap.String("stateDir", stateDir))
			errCh <- publicApp.Listen(runtimePublicListen)
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
