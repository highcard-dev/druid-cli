package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type QueueHandler struct {
	QueueManager ports.QueueManagerInterface
}

func NewQueueHandler(queueManager ports.QueueManagerInterface) *ScrollHandler {
	return &ScrollHandler{QueueManager: queueManager}
}

// @Summary Get current scroll
// @ID getQueue
// @Tags queue, druid, daemon
// @Accept */*
// @Produce json
// @Success 200 {object} map[string]domain.ScrollLockStatus
// @Router /api/v1/queue [get]
func (sl ScrollHandler) Queue(c *fiber.Ctx) error {
	return c.JSON(sl.QueueManager.GetQueue())
}
