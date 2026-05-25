package handlers

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type RouteHandlers struct {
	Server     *RuntimeServer
	Websocket  *WebsocketHandler
	Authorizer ports.AuthorizerServiceInterface
}

type RuntimeServer struct {
	*HealthHandler
	*ScrollHandler
}

func NewRuntimeServer(health *HealthHandler, scrolls *ScrollHandler) *RuntimeServer {
	return &RuntimeServer{HealthHandler: health, ScrollHandler: scrolls}
}

func RegisterRoutes(app *fiber.App, handlers RouteHandlers) {
	RegisterManagementRoutes(app, handlers)
	RegisterPublicRoutes(app, handlers)
}

func RegisterManagementRoutes(app *fiber.App, handlers RouteHandlers) {
	if handlers.Authorizer != nil {
		app.Use(func(ctx *fiber.Ctx) error {
			if _, err := handlers.Authorizer.CheckHeader(ctx); err != nil {
				return fiber.NewError(fiber.StatusUnauthorized, err.Error())
			}
			return ctx.Next()
		})
	}
	api.RegisterHandlersWithOptions(app, handlers.Server, api.FiberServerOptions{})
	app.Get("/health", handlers.Server.GetHealthAuth)
	app.Get("/ws/v1/scrolls/:id/consoles/:console", websocket.New(handlers.Websocket.AttachConsole))
	app.Get("/ws/v1/scrolls/:id/watch/notify", websocket.New(handlers.Websocket.WatchNotifications))
	app.Get("/api/v1/scrolls/:id/dev/status", handlers.Server.GetDaemonWatchStatus)
	app.Post("/api/v1/scrolls/:id/dev/enable", handlers.Server.EnableDaemonWatch)
	app.Post("/api/v1/scrolls/:id/dev/disable", handlers.Server.DisableDaemonWatch)
}

func RegisterPublicRoutes(app *fiber.App, handlers RouteHandlers) {
	var authorizer ports.AuthorizerServiceInterface
	if handlers.Server != nil && handlers.Server.ScrollHandler != nil {
		authorizer = handlers.Server.ScrollHandler.authorizer
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins:  "*",
		AllowMethods:  "GET,POST,PUT,DELETE,PATCH,OPTIONS,HEAD,PROPFIND,MOVE,MKCOL,COPY",
		AllowHeaders:  "Origin,Content-Type,Accept,Authorization,X-Requested-With,Cache-Control,DNT,Keep-Alive,User-Agent,If-Modified-Since,Depth,Destination,Overwrite,If,Lock-Token,Timeout,Dav",
		ExposeHeaders: "Druid-Version",
	}))
	app.Get("/health", handlers.Server.GetHealthAuth)
	app.Get("/.well-known/jwks.json", RuntimeJWKS(authorizer))
	app.Get("/:id/ws/v1/serve/:console", websocket.New(handlers.Websocket.AttachScrollConsole))
	app.Get("/:id/ws/v1/watch/notify", websocket.New(handlers.Websocket.WatchNotificationsPublic))
	if handlers.Server != nil && handlers.Server.ScrollHandler != nil {
		app.Use("/:id", handlers.Server.PublicAuth)
	}
	app.Get("/:id/api/v1/health", handlers.Server.GetHealthAuth)
	app.Get("/:id/api/v1/token", handlers.Server.CreateDaemonToken)
	app.Get("/:id/api/v1/scroll", handlers.Server.GetDaemonScroll)
	app.Put("/:id/api/v1/scroll/commands/:command", handlers.Server.AddDaemonCommand)
	app.Post("/:id/api/v1/command", handlers.Server.RunDaemonCommand)
	app.Get("/:id/api/v1/queue", handlers.Server.GetDaemonQueue)
	app.Get("/:id/api/v1/procedures", handlers.Server.GetDaemonProcedures)
	app.Get("/:id/api/v1/consoles", handlers.Server.GetDaemonConsoles)
	app.Get("/:id/api/v1/logs", handlers.Server.GetDaemonLogs)
	app.Get("/:id/api/v1/logs/:stream", handlers.Server.GetDaemonStreamLogs)
	app.Get("/:id/api/v1/ports", handlers.Server.GetDaemonPorts)
	app.Get("/:id/api/v1/ui/packages", handlers.Server.GetDaemonUIPackages)
	app.Post("/:id/api/v1/ui/packages/:scope/publish", handlers.Server.PublishDaemonUIPackage)
	app.Get("/:id/api/v1/watch/status", handlers.Server.GetDaemonWatchStatus)
	app.Post("/:id/api/v1/watch/enable", handlers.Server.EnableDaemonWatch)
	app.Post("/:id/api/v1/watch/disable", handlers.Server.DisableDaemonWatch)
}
