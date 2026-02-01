package ports

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

type ScrollHandlerInterface interface {
	GetScroll(c *fiber.Ctx) error
	RunCommand(c *fiber.Ctx) error
	RunProcedure(c *fiber.Ctx) error
	GetProcedures(c *fiber.Ctx) error
	AddCommand(c *fiber.Ctx, command string) error
}

type ScrollLogHandlerInterface interface {
	ListAllLogs(c *fiber.Ctx) error
	ListStreamLogs(c *fiber.Ctx, stream string) error
}

type ScrollMetricHandlerInterface interface {
	GetMetrics(c *fiber.Ctx) error
	GetPsTree(c *fiber.Ctx) error
}

type AnnotationHandlerInterface interface {
	Annotations(c *fiber.Ctx) error
}

type WebsocketHandlerInterface interface {
	CreateToken(c *fiber.Ctx) error
	HandleProcess(c *websocket.Conn)
	GetConsoles(c *fiber.Ctx) error
}

type ProcessHandlerInterface interface {
	GetProcesses(c *fiber.Ctx) error
}

type QueueHandlerInterface interface {
	GetQueue(c *fiber.Ctx) error
}

type PortHandlerInterface interface {
	GetPorts(c *fiber.Ctx) error
}
type HealthHandlerInterface interface {
	GetHealthAuth(c *fiber.Ctx) error
}

type ColdstarterHandlerInterface interface {
	FinishColdstarter(c *fiber.Ctx) error
}

type SignalHandlerInterface interface {
	StopDaemon(c *fiber.Ctx) error
}

type WatchHandlerInterface interface {
	EnableWatch(c *fiber.Ctx) error
	DisableWatch(c *fiber.Ctx) error
	GetWatchStatus(c *fiber.Ctx) error
	NotifyChange(c *websocket.Conn)
}
