package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/logger"
	"go.uber.org/zap"
)

type ScrollHandler struct {
	ScrollService   ports.ScrollServiceInterface
	PluginManager   ports.PluginManagerInterface
	ProcessLauncher ports.ProcessLauchnerInterface
}

type StartScrollRequestBody struct {
	ProcessId string `json:"process"`
	CommandId string `json:"command"`
	Sync      bool   `json:"sync"`
}

type StartProcedureRequestBody struct {
	Mode    string `json:"mode"`
	Data    string `json:"data"`
	Process string `json:"process"`
	Sync    bool   `json:"sync"`
}

func NewScrollHandler(scrollService ports.ScrollServiceInterface, pluginManager ports.PluginManagerInterface, processLauncher ports.ProcessLauchnerInterface) *ScrollHandler {
	return &ScrollHandler{ScrollService: scrollService, PluginManager: pluginManager, ProcessLauncher: processLauncher}
}

// @Summary Get current scroll
// @ID getScroll
// @Tags scroll, druid, daemon
// @Accept */*
// @Produce json
// @Success 200 {object} domain.File
// @Router /api/v1/scroll [get]
func (sl ScrollHandler) GetScroll(c *fiber.Ctx) error {
	return c.JSON(sl.ScrollService.GetFile())
}

// @Summary Get current scroll
// @ID runCommand
// @Tags scroll, druid, daemon
// @Accept */*
// @Param body body StartScrollRequestBody true "Scroll Body"
// @Produce json
// @Success 200
// @Success 201
// @Failure 400
// @Failure 500
// @Router /api/v1/command [post]
func (sl ScrollHandler) RunCommand(c *fiber.Ctx) error {

	var requestBody StartScrollRequestBody

	err := c.BodyParser(&requestBody)
	if err != nil {
		return c.SendStatus(400)
	}

	if requestBody.Sync {
		err = sl.ProcessLauncher.RunNew(requestBody.CommandId, requestBody.ProcessId, true)
		if err != nil {
			logger.Log().Error("Error running command (sync)", zap.Error(err))
			return c.SendStatus(500)
		}
		return c.SendStatus(200)
	} else {
		go func() {
			err = sl.ProcessLauncher.RunNew(requestBody.CommandId, requestBody.ProcessId, true)
			if err != nil {
				logger.Log().Error("Error running command (async)", zap.Error(err))
			}
		}()
		c.SendStatus(201)
		return nil
	}

}

// @Summary Run procedure
// @ID runProcedure
// @Tags scroll, druid, daemon
// @Accept */*
// @Param body body StartProcedureRequestBody true "Procedure Body"
// @Produce json
// @Success 201
// @Success 200 {object} any
// @Router /api/v1/procedure [post]
func (sl ScrollHandler) RunProcedure(c *fiber.Ctx) error {
	var requestBody StartProcedureRequestBody

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
	procedure := domain.Procedure{
		Data: requestBody.Data,
		Mode: requestBody.Mode,
	}
	if !requestBody.Sync {

		go sl.ProcessLauncher.RunProcedure(&procedure, requestBody.Process, "tmp")
		return c.SendStatus(201)
	} else {
		res, _, err := sl.ProcessLauncher.RunProcedure(&procedure, requestBody.Process, "tmp")
		if err != nil {
			c.SendString(err.Error())
			return c.SendStatus(400)
		}
		return c.JSON(res)
	}
}
