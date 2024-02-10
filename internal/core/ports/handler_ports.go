package ports

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

type ScrollHandlerInterface interface {
	GetScroll(c *fiber.Ctx) error
	RunCommand(c *fiber.Ctx) error
	RunProcedure(c *fiber.Ctx) error
}

type ScrollLogHandlerInterface interface {
	ListAllLogs(c *fiber.Ctx) error
	ListStreamLogs(c *fiber.Ctx) error
}

type ScrollMetricHandlerInterface interface {
	Metrics(c *fiber.Ctx) error
	PsTree(c *fiber.Ctx) error
}

type WebsocketHandlerInterface interface {
	CreateToken(c *fiber.Ctx) error
	HandleProcess(c *websocket.Conn)
	Consoles(c *fiber.Ctx) error
}
