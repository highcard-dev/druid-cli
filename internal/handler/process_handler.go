package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type ProcessHandler struct {
	ProcessManager ports.ProcessManagerInterface
}

func domainProcessToAPI(dp *domain.Process) api.Process {
	return api.Process{
		Name: dp.Name,
		Type: dp.Type,
	}
}

func NewProcessHandler(processManager ports.ProcessManagerInterface) *ProcessHandler {
	return &ProcessHandler{ProcessManager: processManager}
}

func (ph ProcessHandler) Processes(c *fiber.Ctx) error {
	processes := ph.ProcessManager.GetRunningProcesses()

	// Convert domain processes to API processes
	apiProcesses := make(map[string]api.Process, len(processes))
	for k, v := range processes {
		apiProcesses[k] = domainProcessToAPI(v)
	}

	return c.JSON(api.ProcessesResponse{Processes: apiProcesses})
}
