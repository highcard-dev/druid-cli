package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type HealhResponse struct {
	Mode      string     `json:"mode"`
	Progress  float64    `json:"progress"`
	StartDate *time.Time `json:"start_date,omitempty"`
}

type HealthHandler struct {
	portService     ports.PortServiceInterface
	timeoutDone     bool
	Started         *time.Time
	snapshotService ports.SnapshotService
}

func NewHealthHandler(
	portService ports.PortServiceInterface,
	timeoutSec uint,
	snapshotService ports.SnapshotService,
) *HealthHandler {

	h := &HealthHandler{
		portService,
		false,
		nil,
		snapshotService,
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
// @Success 200 {object} HealhResponse
// @Success 503 {object} HealhResponse
// @Router /api/v1/health [get]
func (p *HealthHandler) Health(c *fiber.Ctx) error {

	portsOpen := p.portService.MandatoryPortsOpen()

	if !p.timeoutDone && !portsOpen {
		c.SendStatus(503)
		return c.JSON(HealhResponse{
			Mode: "manditory_ports",
		})

	}
	if p.Started == nil {
		return c.JSON(HealhResponse{
			Mode: "idle",
		})
	}

	if p.snapshotService.GetCurrentMode() != ports.SnapshotModeNoop {
		pt := p.snapshotService.GetCurrentProgressTracker()
		var perc float64
		if pt != nil {
			perc = (*pt).GetPercent()
		}

		return c.JSON(HealhResponse{
			Mode:     string(p.snapshotService.GetCurrentMode()),
			Progress: perc,
		})
	}

	return c.JSON(HealhResponse{
		Mode:      "ok",
		StartDate: p.Started,
	})
}

func (p *HealthHandler) countdown(timeout *time.Timer) {
	<-timeout.C
	p.timeoutDone = true
}
