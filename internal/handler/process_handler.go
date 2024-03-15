package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type ProcessHandler struct {
	ProcessManager ports.ProcessManagerInterface
}

type ProcessesBody struct {
	Processes map[string]*domain.Process `json:"processes"`
}

func NewProcessHandler(processManager ports.ProcessManagerInterface) *ProcessHandler {
	return &ProcessHandler{ProcessManager: processManager}
}

// @Summary Get running processes
// @ID getRunningProcesses
// @Tags process, druid, daemon
// @Accept */*
// @Produce json
// @Success 200 {object} ProcessesBody
// @Router /api/v1/processes [get]
func (ph ProcessHandler) Processes(c *fiber.Ctx) error {
	processes := ph.ProcessManager.GetRunningProcesses()

	return c.JSON(ProcessesBody{Processes: processes})
}
