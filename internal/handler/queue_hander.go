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

func (sl ScrollHandler) Queue(c *fiber.Ctx) error {
	return c.JSON(sl.QueueManager.GetQueue())
}
