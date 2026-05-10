package handlers

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
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
}

func RegisterPublicRoutes(app *fiber.App, handlers RouteHandlers) {
	app.Get("/health", handlers.Server.GetHealthAuth)
	app.Get("/:id/ws/v1/serve/:console", websocket.New(handlers.Websocket.AttachScrollConsole))
	app.Get("/:id/api/v1/health", handlers.Server.GetHealthAuth)
	app.Get("/:id/api/v1/scroll", handlers.Server.GetDaemonScroll)
	app.Post("/:id/api/v1/command", handlers.Server.RunDaemonCommand)
	app.Get("/:id/api/v1/queue", handlers.Server.GetDaemonQueue)
	app.Get("/:id/api/v1/procedures", handlers.Server.GetDaemonProcedures)
	app.Get("/:id/api/v1/consoles", handlers.Server.GetDaemonConsoles)
	app.Get("/:id/api/v1/logs", handlers.Server.GetDaemonLogs)
	app.Get("/:id/api/v1/logs/:stream", handlers.Server.GetDaemonStreamLogs)
	app.Get("/:id/api/v1/ports", handlers.Server.GetDaemonPorts)
	app.All("/:id/webdav/*", handlers.Server.ServeDaemonWebDAV)
}
