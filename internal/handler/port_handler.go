package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils"
)

type PortHandler struct {
	portService ports.PortServiceInterface
}

func NewPortHandler(
	portService ports.PortServiceInterface,
) *PortHandler {
	return &PortHandler{
		portService,
	}
}

func (p PortHandler) GetPorts(c *fiber.Ctx) error {
	augmentedPorts := p.portService.GetPorts()

	return c.JSON(augmentedPorts)
}

func (p PortHandler) AddPort(c *fiber.Ctx) error {
	var req api.AddPortRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(api.ErrorResponse{
			Status: "error",
			Error:  "invalid request body: " + err.Error(),
		})
	}

	port := domain.Port{
		Port:          req.Port,
		Protocol:      string(req.Protocol),
		Name:          req.Name,
		Mandatory:     utils.BoolValue(req.Mandatory),
		CheckActivity: utils.BoolValue(req.CheckActivity),
		Description:   utils.StringValue(req.Description),
	}

	augmentedPort, err := p.portService.AddPort(port)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(api.ErrorResponse{
			Status: "error",
			Error:  err.Error(),
		})
	}

	return c.Status(fiber.StatusCreated).JSON(augmentedPort)
}

func (p PortHandler) DeletePort(c *fiber.Ctx, port int) error {
	err := p.portService.RemovePort(port)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(api.ErrorResponse{
			Status: "error",
			Error:  err.Error(),
		})
	}

	return c.SendStatus(fiber.StatusNoContent)
}
