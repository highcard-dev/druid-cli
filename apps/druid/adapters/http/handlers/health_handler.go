package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func (h *HealthHandler) GetHealthAuth(c *fiber.Ctx) error {
	return c.JSON(api.HealthResponse{Mode: "ok"})
}
