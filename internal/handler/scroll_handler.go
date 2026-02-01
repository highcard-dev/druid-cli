package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ScrollHandler struct {
	ScrollService   ports.ScrollServiceInterface
	PluginManager   ports.PluginManagerInterface
	ProcessLauncher ports.ProcedureLauchnerInterface
	QueueManager    ports.QueueManagerInterface
	ProcessManager  ports.ProcessManagerInterface
}

func NewScrollHandler(
	scrollService ports.ScrollServiceInterface,
	pluginManager ports.PluginManagerInterface,
	processLauncher ports.ProcedureLauchnerInterface,
	queueManager ports.QueueManagerInterface,
	processManager ports.ProcessManagerInterface,
) *ScrollHandler {
	return &ScrollHandler{ScrollService: scrollService, PluginManager: pluginManager, ProcessLauncher: processLauncher, QueueManager: queueManager, ProcessManager: processManager}
}

func (sl ScrollHandler) GetScroll(c *fiber.Ctx) error {
	return c.JSON(sl.ScrollService.GetFile())
}

func (sl ScrollHandler) RunCommand(c *fiber.Ctx) error {
	var requestBody api.StartCommandRequest

	err := c.BodyParser(&requestBody)
	if err != nil {
		return c.SendStatus(400)
	}

	// Handle optional Sync field
	sync := false
	if requestBody.Sync != nil {
		sync = *requestBody.Sync
	}

	if sync {
		err = sl.QueueManager.AddTempItem(requestBody.Command)
		if err != nil {
			logger.Log().Error("Error running command (sync)", zap.Error(err))
			return c.SendStatus(500)
		}
		return c.SendStatus(200)
	} else {
		go func() {
			err = sl.QueueManager.AddTempItem(requestBody.Command)
			if err != nil {
				logger.Log().Error("Error running command (async)", zap.Error(err))
			}
		}()
		c.SendStatus(201)
		return nil
	}
}

func (sl ScrollHandler) RunProcedure(c *fiber.Ctx) error {
	var requestBody api.StartProcedureRequest

	err := c.BodyParser(&requestBody)
	if err != nil {
		return c.SendStatus(400)
	}

	if !sl.PluginManager.CanRunStandaloneProcedure(requestBody.Mode) && requestBody.Mode != "stdin" {
		c.SendString("Not allowed to run this mode as standalone procedure.")
		return c.SendStatus(400)
	}
	if requestBody.Data == "" {
		c.SendString("Data cannot be empty")
		return c.SendStatus(400)
	}

	var procedure domain.Procedure
	if requestBody.Mode == "stdin" {
		procedure = domain.Procedure{
			Data: []interface{}{
				requestBody.Process,
				requestBody.Data,
			},
			Mode: requestBody.Mode,
		}
	} else {
		procedure = domain.Procedure{
			Data: requestBody.Data,
			Mode: requestBody.Mode,
		}
	}

	command := requestBody.Process

	// Handle optional Dependencies field
	deps := []string{}
	if requestBody.Dependencies != nil {
		deps = *requestBody.Dependencies
	}

	process := sl.ProcessManager.GetRunningProcess(command)
	if process == nil {
		c.SendString("Running process not found")
		return c.SendStatus(400)
	}

	// Handle optional Sync field
	sync := false
	if requestBody.Sync != nil {
		sync = *requestBody.Sync
	}

	if !sync {
		go sl.ProcessLauncher.RunProcedure(&procedure, command, deps)
		return c.SendStatus(201)
	} else {
		res, _, err := sl.ProcessLauncher.RunProcedure(&procedure, command, deps)
		if err != nil {
			c.SendString(err.Error())
			return c.SendStatus(400)
		}
		return c.JSON(res)
	}
}

func (sh ScrollHandler) GetProcedures(c *fiber.Ctx) error {
	process := sh.ProcessLauncher.GetProcedureStatuses()
	return c.JSON(process)
}

func (sh ScrollHandler) AddCommand(c *fiber.Ctx, command string) error {

	var commands *domain.CommandInstructionSet
	err := c.BodyParser(&commands)
	if err != nil {
		return c.SendStatus(400)
	}
	sh.ScrollService.AddTemporaryCommand(command, commands)

	return c.SendStatus(201)
}
