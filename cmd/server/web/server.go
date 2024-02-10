package web

import (
	"errors"
	"fmt"

	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	jwtware "github.com/gofiber/jwt/v3"
	"github.com/gofiber/swagger"
	"github.com/highcard-dev/daemon/cmd/server/web/middlewares"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/logger"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	_ "github.com/highcard-dev/daemon/docs"
	"go.uber.org/zap"
)

type Server struct {
	corsMiddleware                fiber.Handler
	injectUserMiddleware          fiber.Handler
	tokenAuthenticationMiddleware fiber.Handler
	jwtMiddleware                 fiber.Handler
	scrollHandler                 ports.ScrollHandlerInterface
	scrollLogHandler              ports.ScrollLogHandlerInterface
	scrollMetricHandler           ports.ScrollMetricHandlerInterface
	websocketHandler              ports.WebsocketHandlerInterface
}

func NewServer(
	jwlsUrl string,
	scrollHandler ports.ScrollHandlerInterface,
	scrollLogHandler ports.ScrollLogHandlerInterface,
	scrollMetricHandler ports.ScrollMetricHandlerInterface,
	websocketHandler ports.WebsocketHandlerInterface,
	authorizerService ports.AuthorizerServiceInterface,
) *Server {
	server := &Server{
		corsMiddleware: cors.New(cors.Config{
			AllowOrigins: "*",
			AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-DRUID-USER",
		}),
		injectUserMiddleware:          middlewares.NewUserInjector(),
		scrollHandler:                 scrollHandler,
		scrollLogHandler:              scrollLogHandler,
		scrollMetricHandler:           scrollMetricHandler,
		websocketHandler:              websocketHandler,
		tokenAuthenticationMiddleware: middlewares.TokenAuthentication(authorizerService),
	}

	if jwlsUrl != "" {
		server.jwtMiddleware = jwtware.New(jwtware.Config{
			KeySetURLs: []string{jwlsUrl},
		})
	}

	return server
}

func (s *Server) Initialize() *fiber.App {
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
		DisableStartupMessage: true,
	})

	s.SetAPI(app)

	return app
}

func (s *Server) SetAPI(app *fiber.App) *fiber.App {

	wsRoutes := app.Group("/ws/v1")
	v1 := app.Use(s.corsMiddleware).Group("/api/v1")
	dispatcherRoutes := v1.Group("/")

	if s.jwtMiddleware != nil {
		dispatcherRoutes.Use(s.jwtMiddleware, s.injectUserMiddleware)
	}

	wsRoutes.Use(s.tokenAuthenticationMiddleware)

	//Scroll Group
	dispatcherRoutes.Get("/scroll", s.scrollHandler.GetScroll).Name("scrolls.current")
	dispatcherRoutes.Post("/command", s.scrollHandler.RunCommand).Name("command.start")
	dispatcherRoutes.Post("/procedure", s.scrollHandler.RunProcedure).Name("procedure.start")

	//Scroll Logs Group
	dispatcherRoutes.Get("/logs", s.scrollLogHandler.ListAllLogs).Name("scrolls.logs")
	dispatcherRoutes.Get("/logs/:stream", s.scrollLogHandler.ListStreamLogs).Name("scrolls.log")

	//Authentication Group
	dispatcherRoutes.Get("/token", s.websocketHandler.CreateToken).Name("token.create")

	//Metrics Group
	dispatcherRoutes.Get("/metrics", s.scrollMetricHandler.Metrics).Name("scrolls.metrics")
	dispatcherRoutes.Get("/pstree", s.scrollMetricHandler.PsTree).Name("scrolls.pstree")

	//Websocket Group
	dispatcherRoutes.Get("/consoles", s.websocketHandler.Consoles).Name("consoles.list")
	wsRoutes.Get("/serve/:console", websocket.New(s.websocketHandler.HandleProcess)).Name("ws.serve")

	app.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler())).Name("metrics")

	app.Get("/swagger/*", swagger.HandlerDefault) // default

	//Catch-all 404 page
	app.Use(func(ctx *fiber.Ctx) error {
		return ctx.SendStatus(404)
	})

	return app
}

func (s *Server) Serve(app *fiber.App, port int) {
	addr := fmt.Sprintf(":%d", port)
	if err := app.Listen(addr); err != nil {
		logger.Log().Error("web server error", zap.Error(err))
	}
}
