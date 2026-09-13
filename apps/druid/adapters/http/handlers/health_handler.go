package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
)

type ProgressLookup func(runtimeID string) (float64, bool)

type HealthHandler struct {
	progress ProgressLookup
}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func NewHealthHandlerWithProgress(progress ProgressLookup) *HealthHandler {
	return &HealthHandler{progress: progress}
}

func (h *HealthHandler) GetHealthAuth(c *fiber.Ctx) error {
	health := api.HealthResponse{Mode: "ok"}
	if h.progress != nil {
		if progress, ok := h.progress(c.Params("id")); ok {
			value := float32(progress)
			health.Progress = &value
		}
	}
	return c.JSON(health)
}
