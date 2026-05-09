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
