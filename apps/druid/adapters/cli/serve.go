package cli

import (
	"net"
	"os"
	"path/filepath"

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
var k8sNamespace string
var k8sStorageClass string
var k8sPullImage string
var k8sRegistrySecret string
var hubbleRelayAddr string
var k8sKubeconfig string

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
	supervisor := appservices.NewRuntimeSupervisor(store, manager, consoleService, runtimeBackend, runtimebackend.WithKubernetesConfig(kubernetesConfig))
	if err := supervisor.Start(); err != nil {
		return err
	}

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	runtimehandlers.RegisterRoutes(app, runtimehandlers.RouteHandlers{
		Server: runtimehandlers.NewRuntimeServer(
			runtimehandlers.NewHealthHandler(),
			runtimehandlers.NewScrollHandler(supervisor),
		),
		Websocket: runtimehandlers.NewWebsocketHandler(consoleService),
	})

	return listenRuntimeDaemon(app, store.StateDir())
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
