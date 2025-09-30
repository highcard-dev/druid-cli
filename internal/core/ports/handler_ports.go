package ports

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

type ScrollHandlerInterface interface {
	GetScroll(c *fiber.Ctx) error
	RunCommand(c *fiber.Ctx) error
	RunProcedure(c *fiber.Ctx) error
	Procedures(c *fiber.Ctx) error
}

type ScrollLogHandlerInterface interface {
	ListAllLogs(c *fiber.Ctx) error
	ListStreamLogs(c *fiber.Ctx) error
}

type ScrollMetricHandlerInterface interface {
	Metrics(c *fiber.Ctx) error
	PsTree(c *fiber.Ctx) error
}

type AnnotationHandlerInterface interface {
	Annotations(c *fiber.Ctx) error
}

type WebsocketHandlerInterface interface {
	CreateToken(c *fiber.Ctx) error
	HandleProcess(c *websocket.Conn)
	Consoles(c *fiber.Ctx) error
}

type ProcessHandlerInterface interface {
	Processes(c *fiber.Ctx) error
}

type QueueHandlerInterface interface {
	Queue(c *fiber.Ctx) error
}

type PortHandlerInterface interface {
	GetPorts(c *fiber.Ctx) error
}
type HealthHandlerInterface interface {
	Health(ctx *fiber.Ctx) error
}

type ColdstarterHandlerInterface interface {
	Finish(ctx *fiber.Ctx) error
}

type SignalHandlerInterface interface {
	Stop(ctx *fiber.Ctx) error
}

type UiHandlerInterface interface {
	PublicIndex(ctx *fiber.Ctx) error
	PrivateIndex(ctx *fiber.Ctx) error
}

type UiDevHandlerInterface interface {
	Enable(ctx *fiber.Ctx) error
	Disable(ctx *fiber.Ctx) error
	Status(ctx *fiber.Ctx) error
	NotifyChange(c *websocket.Conn)
}
