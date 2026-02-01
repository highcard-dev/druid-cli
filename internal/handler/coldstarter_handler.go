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

func (ah ColdstarterHandler) FinishColdstarter(c *fiber.Ctx) error {
	ah.coldstarter.Finish(nil)
	c.Status(202)
	return nil
}
