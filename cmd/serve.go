package cmd

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"time"

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
var watchPorts bool
var watchPortsInterfaces []string
var portInactivity uint
var useColdstarter bool
var maxStartupHealthCheckTimeout uint

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

		ctx := cmd.Context()

		disablePrometheus, ok := ctx.Value("disablePrometheus").(bool)

		//only disable prometheus if context value is set and true
		processMonitor := services.NewProcessMonitor(!ok || !disablePrometheus)

		defer processMonitor.ShutdownPromMetrics()

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
				return fmt.Errorf("failed to load scroll - %w", err)
			}
		}

		currentScroll := scrollService.GetCurrent()

		logger.Log().Info("Scroll loaded", zap.String("Name", currentScroll.Name), zap.Any("Version", currentScroll.Version), zap.String("AppVersion", currentScroll.AppVersion), zap.Any("Ports", currentScroll.Ports))

		processLauncher := services.NewProcedureLauncher(client, processManager, pluginManager, consoleService, logManager, scrollService)

		queueManager := services.NewQueueManager(scrollService, processLauncher)

		portService := services.NewPortServiceWithScrollFile(scrollService.GetFile())

		coldStarter := services.NewColdStarter(portService, queueManager, scrollService.GetDir())

		scrollHandler := handler.NewScrollHandler(scrollService, pluginManager, processLauncher, queueManager)
		processHandler := handler.NewProcessHandler(processManager)
		scrollLogHandler := handler.NewScrollLogHandler(scrollService, logManager, processManager)
		scrollMetricHandler := handler.NewScrollMetricHandler(scrollService, processMonitor)
		queueHandler := handler.NewQueueHandler(queueManager)
		portHandler := handler.NewPortHandler(portService)
		healthHandler := handler.NewHealthHandler(portService, maxStartupHealthCheckTimeout)
		coldstarterHandler := handler.NewColdstarterHandler(coldStarter)

		var annotationHandler *handler.AnnotationHandler = nil

		if slices.Contains(additionalEndpoints, "annotations") {
			annotationHandler = handler.NewAnnotationHandler(scrollService)
		}

		websocketHandler := handler.NewWebsocketHandler(authorizer, scrollService, consoleService)

		s := web.NewServer(jwksUrl, scrollHandler, scrollLogHandler, scrollMetricHandler, annotationHandler, processHandler, queueHandler, websocketHandler, portHandler, healthHandler, coldstarterHandler, authorizer, cwd)

		a := s.Initialize()

		signalHandler := signals.NewSignalHandler(ctx, queueManager, processManager, a, shutdownWait)

		if watchPorts {
			logger.Log().Info("Starting port watcher", zap.Strings("interfaces", watchPortsInterfaces))
			go portService.StartMonitoring(ctx, watchPortsInterfaces, currentScroll.KeepAlivePPM)
		}

		if !idleScroll {

			doneChan := make(chan error, 1)
			go func() {
				for {
					err := <-doneChan
					if err != nil {
						logger.Log().Error("Error in Daemon Startup", zap.Error(err))
						signalHandler.Stop()
					}
					logger.Log().Info("Daemon Startup Complete")
				}
			}()

			go func() {
				if useColdstarter {
					if currentScroll.CanColdStart() {

						for {
							healthHandler.Started = false
							logger.Log().Info("Starting coldstarter")
							finish := coldStarter.Start(ctx)
							executedPort := <-finish

							if executedPort == nil || executedPort.FinishAfterCommand == "" {
								coldStarter.Stop(executedPort.StartDelay)
							}

							logger.Log().Info("Coldstarter done, starting scroll")

							startup(scrollService, processLauncher, queueManager, portService, coldStarter, healthHandler, cwd, doneChan)

							portService.ResetOpenPorts()

							if !watchPorts {
								logger.Log().Warn("watch-port is disabled, skipping inactivty watch")
								return
							}

							logger.Log().Info("Waiting for inactivity..")

							for {

								if ctx.Err() != nil {
									logger.Log().Info("Context cancelled, stopping shutdown")
									break
								}

								ports := portService.GetPorts()
								inactive := true
								for _, port := range ports {
									if port.InactiveSinceSec < portInactivity {
										logger.Log().Info("Port still active", zap.Int("port", port.Port.Port), zap.Uint("InactiveSinceSec", port.InactiveSinceSec))
										inactive = false
										break
									}
								}
								if inactive {
									logger.Log().Info("Ports inactive, starting shutdown")
									break
								} else {
									logger.Log().Info("Ports still active, waiting..")
									time.Sleep(5 * time.Second)
								}
							}

							signalHandler.ShutdownRoutine()
						}
					} else {
						logger.Log().Warn("No ports to start, skipping coldstarter")
						startup(scrollService, processLauncher, queueManager, portService, coldStarter, healthHandler, cwd, doneChan)
					}
				} else {
					startup(scrollService, processLauncher, queueManager, portService, coldStarter, healthHandler, cwd, doneChan)
				}

			}()

		} else {
			if useColdstarter {
				go coldStarter.Start(ctx)
			}
		}

		err = s.Serve(a, port)

		logger.Log().Info("Shutting down")

		return err
	},
}

func init() {
	ServeCommand.Flags().IntVarP(&port, "port", "p", 8081, "Port")

	ServeCommand.Flags().IntVarP(&shutdownWait, "shutdown-wait", "", 60, "Wait interval how long the process is allowed to shutdown. First normal shutdown, then forced shutdown")

	ServeCommand.Flags().StringVarP(&jwksUrl, "jwks-server", "", "", "JWKS Server to authenticate requests against")

	ServeCommand.Flags().StringVarP(&userId, "user-id", "u", "", "Allowed user id")

	ServeCommand.Flags().BoolVarP(&idleScroll, "idle", "", false, "Don't start the queue manager")

	ServeCommand.Flags().BoolVarP(&watchPorts, "watch-ports", "", false, "Watch ports, even when coldstarter is not active")

	//macOS specific
	if runtime.GOOS == "darwin" {
		ServeCommand.Flags().StringArrayVarP(&watchPortsInterfaces, "watch-ports-interfaces", "", []string{"lo0", "en0"}, "Interfaces to watch for port activity")
	} else {
		ServeCommand.Flags().StringArrayVarP(&watchPortsInterfaces, "watch-ports-interfaces", "", []string{"lo"}, "Interfaces to watch for port activity")
	}

	ServeCommand.Flags().BoolVarP(&useColdstarter, "coldstarter", "", false, "Use coldstarter to not start immediately")

	ServeCommand.Flags().BoolVarP(&ignoreVersionCheck, "ignore-version-check", "", false, "Ignore version check")

	ServeCommand.Flags().StringArrayVarP(&additionalEndpoints, "additional-endpoints", "", []string{}, "Additional endpoints to serve. Valid values: annotations")

	ServeCommand.Flags().UintVarP(&portInactivity, "port-inactivity", "", 120, "Port inactivity timeout")

	ServeCommand.Flags().UintVarP(&maxStartupHealthCheckTimeout, "max-health-check-startup-timeount", "", 0, "Sets the max amount of time the health check is allowed to take on startup. If the value is 0, there will be no timeout. This is useful to prevent the health check from blocking the startup of the daemon fully.")
}

func startup(scrollService *services.ScrollService, processLauncher *services.ProcedureLauncher, queueManager *services.QueueManager, portSerivce *services.PortMonitor, coldStarter *services.ColdStarter, healthHandler *handler.HealthHandler, cwd string, doneChan chan error) {

	healthHandler.Started = true

	currentScroll := scrollService.GetCurrent()

	lock, err := scrollService.Bootstrap(ignoreVersionCheck)
	if err != nil {
		doneChan <- err
		return
	}

	newScroll := len(lock.Statuses) == 0

	if newScroll {
		logger.Log().Info("No lock file found, but init command available. Bootstrapping...")

		logger.Log().Info("Creating lock and bootstrapping files")
		//There is an error here. We need to bootstrap the files before we render out the templates in the bootstrap func above
		err := scrollService.CreateLockAndBootstrapFiles()
		if err != nil {
			doneChan <- err
			return
		}
	} else {
		logger.Log().Info("Found lock file, bootstrapping done")
	}

	logger.Log().Info("Rendering cwd templates")
	err = scrollService.RenderCwdTemplates()
	if err != nil {
		doneChan <- err
		return
	}

	logger.Log().Info("Launching plugins")
	//important to launch plugins, after the templates are rendered, sothat templates can provide for plugins
	err = processLauncher.LaunchPlugins()

	if err != nil {
		doneChan <- err
		return
	}

	logger.Log().Info("Starting queue manager")
	go queueManager.Work()

	if newScroll {
		logger.Log().Info("Starting scroll.init process")
		//start scroll.init process
		//initialize if nothing is there
		err = queueManager.AddAndRememberItem(currentScroll.Init)
		if err != nil {
			doneChan <- err
			return
		}

		logger.Log().Info("Writing new scroll lock")
		scrollService.WriteNewScrollLock()

		logger.Log().Info("Bootstrapping done")
	}

	callbacks := map[string]func(){}

	for _, port := range portSerivce.GetPorts() {
		if port.FinishAfterCommand != "" {
			callbacks[port.FinishAfterCommand] = func() {
				coldStarter.Stop(port.StartDelay)
			}
		}
	}

	queueManager.RegisterCallbacks(callbacks)

	err = queueManager.QueueLockFile()
	if err != nil {
		doneChan <- err
		return
	}

	//schedule crons
	logger.Log().Info("Schedule crons")

	cronManager := services.NewCronManager(currentScroll.Cronjobs, queueManager)
	err = cronManager.Init()

	if err != nil {
		doneChan <- err
		return
	}

	var version string

	if currentScroll.Version != nil {
		version = currentScroll.Version.String()
	} else {
		version = "N/A"
	}

	logger.Log().Info("Active Scroll",
		zap.String("Description", fmt.Sprintf("%s (%s)", currentScroll.Desc, currentScroll.Name)),
		zap.String("Scroll Version", version),
		zap.String("cwd", cwd))

	doneChan <- nil

}
