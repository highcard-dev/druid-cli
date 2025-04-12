package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
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

// @Summary Get ports from scroll with additional information
// @ID getPorts
// @Tags port, druid, daemon
// @Accept */*
// @Produce json
// @Success 200 {object} []domain.AugmentedPort
// @Router /api/v1/ports [get]
func (p PortHandler) GetPorts(c *fiber.Ctx) error {
	augmentedPorts := p.portService.GetPorts()

	return c.JSON(augmentedPorts)
}
