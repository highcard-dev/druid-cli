package handlers

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
)

type RouteHandlers struct {
	Server    *RuntimeServer
	Websocket *WebsocketHandler
}

type RuntimeServer struct {
	*HealthHandler
	*ScrollHandler
}

func NewRuntimeServer(health *HealthHandler, scrolls *ScrollHandler) *RuntimeServer {
	return &RuntimeServer{HealthHandler: health, ScrollHandler: scrolls}
}

func RegisterRoutes(app *fiber.App, handlers RouteHandlers) {
	api.RegisterHandlersWithOptions(app, handlers.Server, api.FiberServerOptions{})
	app.Get("/health", handlers.Server.GetHealthAuth)
	app.Get("/ws/v1/scrolls/:id/consoles/:console", websocket.New(handlers.Websocket.AttachConsole))
}
