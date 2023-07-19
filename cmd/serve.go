package cmd

import (
	"fmt"

	"github.com/highcard-dev/daemon/cmd/server/web"
	"github.com/highcard-dev/daemon/internal/core/services"
	logger "github.com/highcard-dev/daemon/internal/core/services/log"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/handler"
	"github.com/highcard-dev/daemon/internal/signals"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var jwksUrl, userId string
var ignoreVersionCheck bool
var port int
var shutdownWait int

var ServeCommand = &cobra.Command{
	Use:   "serve",
	Short: "Initiate a Server and Communication with the Application",
	Long: `This command locks the terminal by starting the Daemon,
which in turn compiles the scroll file, enable the API and Websocket
to interact and monitor the Scroll Application`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		var artifact string
		if len(args) > 0 {
			artifact = args[0]
		}

		logger.Log().Info("Starting Scroll Daemon")
		authorizer, err := services.NewAuthorizer(jwksUrl, userId)
		if err != nil {
			return err
		}

		host := viper.GetString("registry.host")
		user := viper.GetString("registry.user")
		password := viper.GetString("registry.password")

		client := registry.NewOciClient(host, user, password)
		logManager := services.NewLogManager()
		hub := services.NewHub()
		processManager := services.NewProcessManager(logManager, hub)
		processMonitor := services.NewProcessMonitor(processManager)

		pluginManager := services.NewPluginManager()

		logger.Log().Info("Starting Process Monitor")
		go processMonitor.StartMonitoring()

		scrollService := services.NewScrollService(cwd, client, logManager, processManager, hub, pluginManager)

		if !scrollService.ScrollExists() {

			if artifact == "" {
				return fmt.Errorf("no artifact provided")
			}

			logger.Log().Info("Scroll does not exist... downloading " + artifact + " into " + scrollService.GetDir())

			if err != nil {
				return err
			}

			err = client.Pull(scrollService.GetDir(), artifact)
			if err != nil {
				return err
			}

			logger.Log().Info("Installed scroll " + artifact)
		}

		scrollHandler := handler.NewScrollHandler(scrollService, pluginManager)
		scrollLogHandler := handler.NewScrollLogHandler(scrollService, logManager)
		scrollMetricHandler := handler.NewScrollMetricHandler(scrollService, processMonitor)
		websocketHandler := handler.NewWebsocketHandler(authorizer, scrollService, hub)

		s := web.NewServer(jwksUrl, scrollHandler, scrollLogHandler, scrollMetricHandler, websocketHandler, authorizer)

		a := s.Initialize()

		signals.SetupSignals(scrollService, a, shutdownWait)

		currentScroll, err := scrollService.Load(ignoreVersionCheck)

		if err != nil {
			return err
		}

		err = scrollService.StartLockfile()

		if err != nil {
			return err
		}

		logger.Log().Info("Active Scroll",
			zap.String("Description", fmt.Sprintf("%s (%s)", currentScroll.Desc, currentScroll.Name)),
			zap.String("Scroll Version", currentScroll.Version.String()),
			zap.String("cwd", cwd))

		if !scrollService.GetLock().Initialized {
			logger.Log().Info("Running init-command command")
			scrollService.Initalize()
		}

		logger.Log().Info("Bootstrapping done")

		s.Serve(a, port)

		return nil
	},
}

func init() {
	ServeCommand.Flags().IntVarP(&port, "port", "p", 8081, "Port")

	ServeCommand.Flags().IntVarP(&shutdownWait, "shutdown-wait", "", 60, "Wait interval how long the process is allowed to shutdown. First normal shutdown, then forced shutdown")

	ServeCommand.Flags().StringVarP(&jwksUrl, "jwks-server", "", "", "JWKS Server to authenticate requests against")

	ServeCommand.Flags().StringVarP(&userId, "user-id", "u", "", "Allowed user id")

}
