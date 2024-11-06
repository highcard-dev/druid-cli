package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type ColdstarterHandler struct {
	coldstarter ports.ColdStarterInterface
}

func NewColdstarterHandler(coldstarter ports.ColdStarterInterface) *ColdstarterHandler {
	return &ColdstarterHandler{
		coldstarter: coldstarter,
	}
}

// @Summary Finish Coldstarter
// @ID finishColdStarter
// @Tags coldstarter, druid, daemon
// @Accept */*
// @Success 202
// @Router /api/v1/coldstarter/finish [POST]
func (ah ColdstarterHandler) Finish(c *fiber.Ctx) error {
	ah.coldstarter.Finish(nil)
	c.Status(202)
	return nil
}
