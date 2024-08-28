package web

import (
	"errors"
	"fmt"

	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	jwtware "github.com/gofiber/jwt/v3"
	"github.com/highcard-dev/daemon/cmd/server/web/middlewares"
	constants "github.com/highcard-dev/daemon/internal"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/webdav"

	_ "github.com/highcard-dev/daemon/docs"
	"go.uber.org/zap"
)

type Server struct {
	corsMiddleware                fiber.Handler
	injectUserMiddleware          fiber.Handler
	headerMiddleware              fiber.Handler
	tokenAuthenticationMiddleware fiber.Handler
	jwtMiddleware                 fiber.Handler
	scrollHandler                 ports.ScrollHandlerInterface
	scrollLogHandler              ports.ScrollLogHandlerInterface
	scrollMetricHandler           ports.ScrollMetricHandlerInterface
	annotationHandler             ports.AnnotationHandlerInterface
	processHandler                ports.ProcessHandlerInterface
	queueHandler                  ports.QueueHandlerInterface
	websocketHandler              ports.WebsocketHandlerInterface
	portHandler                   ports.PortHandlerInterface
	webdavPath                    string
}

func NewServer(
	jwlsUrl string,
	scrollHandler ports.ScrollHandlerInterface,
	scrollLogHandler ports.ScrollLogHandlerInterface,
	scrollMetricHandler ports.ScrollMetricHandlerInterface,
	annotationHandler ports.AnnotationHandlerInterface,
	processHandler ports.ProcessHandlerInterface,
	queueHandler ports.QueueHandlerInterface,
	websocketHandler ports.WebsocketHandlerInterface,
	portHandler ports.PortHandlerInterface,
	authorizerService ports.AuthorizerServiceInterface,
	webdavPath string,
) *Server {
	server := &Server{
		corsMiddleware: cors.New(cors.Config{
			AllowOrigins: "*",
			AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-DRUID-USER",
		}),
		injectUserMiddleware:          middlewares.NewUserInjector(),
		headerMiddleware:              middlewares.NewHeaderMiddleware(),
		scrollHandler:                 scrollHandler,
		scrollLogHandler:              scrollLogHandler,
		scrollMetricHandler:           scrollMetricHandler,
		annotationHandler:             annotationHandler,
		processHandler:                processHandler,
		queueHandler:                  queueHandler,
		websocketHandler:              websocketHandler,
		portHandler:                   portHandler,
		tokenAuthenticationMiddleware: middlewares.TokenAuthentication(authorizerService),
		webdavPath:                    webdavPath,
	}

	if jwlsUrl != "" {
		server.jwtMiddleware = jwtware.New(jwtware.Config{
			KeySetURLs: []string{jwlsUrl},
		})
	}

	return server
}

func (s *Server) Initialize() *fiber.App {
	webdavRequestMethods := []string{"PROPFIND", "MKCOL", "COPY", "MOVE"}

	app := fiber.New(fiber.Config{
		ErrorHandler: func(ctx *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			var e *fiber.Error
			if errors.As(err, &e) {
				code = e.Code
				return ctx.Status(code).JSON(e)
			} else {
				var e fiber.Error
				e.Code = 500
				e.Message = err.Error()
				return ctx.Status(code).JSON(e)
			}
		},
		RequestMethods:        append(fiber.DefaultMethods[:], webdavRequestMethods...),
		DisableStartupMessage: true,
	})

	s.SetAPI(app)

	return app
}

func (s *Server) SetAPI(app *fiber.App) *fiber.App {
	app.Use(s.headerMiddleware)
	wsRoutes := app.Group("/ws/v1")
	v1 := app.Use(s.corsMiddleware).Group("/api/v1")
	apiRoutes := v1.Group("/")
	webdavRoutes := app.Group("/webdav")

	if s.jwtMiddleware != nil {
		apiRoutes.Use(s.jwtMiddleware, s.injectUserMiddleware)
		webdavRoutes.Use(s.jwtMiddleware, s.injectUserMiddleware)
	}

	wsRoutes.Use(s.tokenAuthenticationMiddleware)

	//Scroll Group
	apiRoutes.Get("/scroll", s.scrollHandler.GetScroll).Name("scrolls.current")
	apiRoutes.Post("/command", s.scrollHandler.RunCommand).Name("command.start")
	apiRoutes.Post("/procedure", s.scrollHandler.RunProcedure).Name("procedure.start")

	//Scroll Logs Group
	apiRoutes.Get("/logs", s.scrollLogHandler.ListAllLogs).Name("scrolls.logs")
	apiRoutes.Get("/logs/:stream", s.scrollLogHandler.ListStreamLogs).Name("scrolls.log")

	//Authentication Group
	apiRoutes.Get("/token", s.websocketHandler.CreateToken).Name("token.create")

	//Metrics Group
	apiRoutes.Get("/metrics", s.scrollMetricHandler.Metrics).Name("scrolls.metrics")
	apiRoutes.Get("/pstree", s.scrollMetricHandler.PsTree).Name("scrolls.pstree")

	//Processes Group
	apiRoutes.Get("/processes", s.processHandler.Processes).Name("processes.list")

	apiRoutes.Get("/queue", s.queueHandler.Queue).Name("queue.list")

	//Websocket Group
	apiRoutes.Get("/consoles", s.websocketHandler.Consoles).Name("consoles.list")

	// Create the WebDAV handler
	webdavHandler := &webdav.Handler{
		Prefix:     "/webdav",
		FileSystem: webdav.Dir(s.webdavPath),
		LockSystem: webdav.NewMemLS(),
	}

	webdavRoutes.Use("*", adaptor.HTTPHandler(webdavHandler))

	wsRoutes.Get("/serve/:console", websocket.New(s.websocketHandler.HandleProcess)).Name("ws.serve")

	apiRoutes.Get("/ports", s.portHandler.GetPorts).Name("ports.list")

	if s.annotationHandler != nil {
		app.Get("/annotations", s.annotationHandler.Annotations).Name("annotations.list")
	}
	app.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler())).Name("metrics")

	app.Get("/info", func(ctx *fiber.Ctx) error {
		return ctx.JSON(fiber.Map{
			"version": constants.Version,
		})
	})

	//app.Get("/swagger/*", swagger.HandlerDefault) // default

	//Catch-all 404 page
	app.Use(func(ctx *fiber.Ctx) error {
		return ctx.SendStatus(404)
	})

	return app
}

func (s *Server) Serve(app *fiber.App, port int) error {
	addr := fmt.Sprintf(":%d", port)
	if err := app.Listen(addr); err != nil {
		logger.Log().Error("web server error", zap.Error(err))
		return err
	}
	return nil
}
