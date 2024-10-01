package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type HealthHandler struct {
	portService ports.PortServiceInterface
	timeoutDone bool
}

func NewHealthHandler(
	portService ports.PortServiceInterface,
	timeoutSec uint,
) *HealthHandler {

	h := &HealthHandler{
		portService,
		false,
	}

	// if timeoutSec == 0, we want at some point to not show a bad health status
	if timeoutSec != 0 {
		timeout := time.NewTimer(time.Duration(timeoutSec) * time.Second)
		go h.countdown(timeout)
	}

	return h
}

// @Summary Get ports from scroll with additional information
// @ID getHealth
// @Tags health, druid, daemon
// @Accept */*
// @Produce json
// @Success 200 {object} string
// @Success 503 {object} string
// @Router /api/v1/health [get]
func (p *HealthHandler) Health(c *fiber.Ctx) error {

	portsOpen := p.portService.MandatoryPortsOpen()

	if !p.timeoutDone && !portsOpen {
		c.SendStatus(503)
		return c.SendString("Manditory ports are not open")

	}

	return c.SendString("ok")
}

func (p *HealthHandler) countdown(timeout *time.Timer) {
	<-timeout.C
	p.timeoutDone = true
}
