package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/signals"
)

type DaemonHandler struct {
	shutdown *signals.SignalHandler
}

func NewDaemonHandler(shutdown *signals.SignalHandler) *DaemonHandler {
	return &DaemonHandler{
		shutdown: shutdown,
	}
}

// @Summary Finish Coldstarter
// @ID finishColdStarter
// @Tags druid, daemon
// @Accept */*
// @Success 202
// @Router /api/v1/daemon/stop [POST]
func (ah DaemonHandler) Stop(c *fiber.Ctx) error {
	ah.shutdown.Stop()
	c.Status(201)
	return nil
}
