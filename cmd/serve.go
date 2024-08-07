package cmd

import (
	"errors"
	"fmt"
	"slices"

	"github.com/highcard-dev/daemon/cmd/server/web"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/handler"
	"github.com/highcard-dev/daemon/internal/signals"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var jwksUrl, userId string
var ignoreVersionCheck bool
var port int
var shutdownWait int
var additionalEndpoints []string
var idleScroll bool

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
		consoleService := services.NewConsoleManager(logManager)
		processMonitor := services.NewProcessMonitor()
		ctx := cmd.Context()
		go func() {
			<-ctx.Done()
			processMonitor.ShutdownPromMetrics()
		}()

		processManager := services.NewProcessManager(logManager, consoleService, processMonitor)

		pluginManager := services.NewPluginManager()

		logger.Log().Info("Starting Process Monitor")
		go processMonitor.StartMonitoring()

		scrollService, err := services.NewScrollService(cwd)
		if err != nil {
			if errors.Is(err, domain.ErrScrollDoesNotExist) {
				logger.Log().Warn("Scroll does not exist.")

				if artifact == "" {
					return fmt.Errorf("no artifact provided")
				}

				logger.Log().Info("Downloading " + artifact + " into " + scrollService.GetDir())

				err = client.Pull(scrollService.GetDir(), artifact)
				if err != nil {
					return err
				}

				_, err = scrollService.LoadScroll()
				if err != nil {
					return err
				}

				logger.Log().Info("Installed scroll " + artifact)
			} else {
				return fmt.Errorf("error creating scroll service: %w", err)
			}
		}

		processLauncher := services.NewProcedureLauncher(client, processManager, pluginManager, consoleService, logManager, scrollService)

		queueManager := services.NewQueueManager(scrollService, processLauncher)

		scrollHandler := handler.NewScrollHandler(scrollService, pluginManager, processLauncher, queueManager)
		processHandler := handler.NewProcessHandler(processManager)
		scrollLogHandler := handler.NewScrollLogHandler(scrollService, logManager, processManager)
		scrollMetricHandler := handler.NewScrollMetricHandler(scrollService, processMonitor)
		queueHandler := handler.NewQueueHandler(queueManager)

		var annotationHandler *handler.AnnotationHandler = nil

		if slices.Contains(additionalEndpoints, "annotations") {
			annotationHandler = handler.NewAnnotationHandler(scrollService)
		}

		websocketHandler := handler.NewWebsocketHandler(authorizer, scrollService, consoleService)

		s := web.NewServer(jwksUrl, scrollHandler, scrollLogHandler, scrollMetricHandler, annotationHandler, processHandler, queueHandler, websocketHandler, authorizer, cwd)

		a := s.Initialize()

		signals.SetupSignals(queueManager, processManager, a, shutdownWait)

		if !idleScroll {

			currentScroll, lock, err := scrollService.Bootstrap(ignoreVersionCheck)
			if err != nil {
				return err
			}

			newScroll := len(lock.Statuses) == 0

			if newScroll {
				logger.Log().Info("No lock file found, but init command available. Bootstrapping...")

				logger.Log().Info("Creating lock and bootstrapping files")
				//There is an error here. We need to bootstrap the files before we render out the templates in the bootstrap func above
				err := scrollService.CreateLockAndBootstrapFiles()
				if err != nil {
					return err
				}
			} else {
				logger.Log().Info("Found lock file, bootstrapping done")
			}

			logger.Log().Info("Rendering cwd templates")
			err = scrollService.RenderCwdTemplates()
			if err != nil {
				return err
			}

			logger.Log().Info("Launching plugins")
			//important to launch plugins, after the templates are rendered, sothat templates can provide for plugins
			err = processLauncher.LaunchPlugins()

			if err != nil {
				return err
			}

			logger.Log().Info("Starting queue manager")
			go queueManager.Work()

			if newScroll {
				logger.Log().Info("Starting scroll.init process")
				//start scroll.init process
				//initialize if nothing is there
				err = queueManager.AddAndRememberItem(currentScroll.Init)
				if err != nil {
					return err
				}

				logger.Log().Info("Writing new scroll lock")
				scrollService.WriteNewScrollLock()

				logger.Log().Info("Bootstrapping done")
			}

			err = queueManager.QueueLockFile()
			if err != nil {
				return err
			}

			//schedule crons
			logger.Log().Info("Schedule crons")

			cronManager := services.NewCronManager(currentScroll.Cronjobs, queueManager)
			err = cronManager.Init()

			if err != nil {
				return err
			}

			logger.Log().Info("Active Scroll",
				zap.String("Description", fmt.Sprintf("%s (%s)", currentScroll.Desc, currentScroll.Name)),
				zap.String("Scroll Version", currentScroll.Version.String()),
				zap.String("cwd", cwd))
		}
		err = s.Serve(a, port)

		return err
	},
}

func init() {
	ServeCommand.Flags().IntVarP(&port, "port", "p", 8081, "Port")

	ServeCommand.Flags().IntVarP(&shutdownWait, "shutdown-wait", "", 60, "Wait interval how long the process is allowed to shutdown. First normal shutdown, then forced shutdown")

	ServeCommand.Flags().StringVarP(&jwksUrl, "jwks-server", "", "", "JWKS Server to authenticate requests against")

	ServeCommand.Flags().StringVarP(&userId, "user-id", "u", "", "Allowed user id")

	ServeCommand.Flags().BoolVarP(&idleScroll, "idle", "", false, "Don't start the queue manager")

	ServeCommand.Flags().BoolVarP(&ignoreVersionCheck, "ignore-version-check", "", false, "Ignore version check")

	ServeCommand.Flags().StringArrayVarP(&additionalEndpoints, "additional-endpoints", "", []string{}, "Additional endpoints to serve. Valid values: annotations")

}
