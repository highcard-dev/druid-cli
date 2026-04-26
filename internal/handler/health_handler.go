package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type HealthHandler struct {
	portService ports.PortServiceInterface
	timeoutDone bool
	Started     *time.Time
	progress    *domain.SnapshotProgress
}

func NewHealthHandler(
	portService ports.PortServiceInterface,
	timeoutSec uint,
	progress *domain.SnapshotProgress,
) *HealthHandler {

	h := &HealthHandler{
		portService: portService,
		timeoutDone: false,
		Started:     nil,
		progress:    progress,
	}

	// if timeoutSec == 0, we want at some point to not show a bad health status
	if timeoutSec != 0 {
		timeout := time.NewTimer(time.Duration(timeoutSec) * time.Second)
		go h.countdown(timeout)
	}

	return h
}

func (p *HealthHandler) GetHealthAuth(c *fiber.Ctx) error {

	if p.progress != nil {
		if mode, ok := p.progress.Mode.Load().(string); ok && mode == "restore" {
			pct := float32(p.progress.Percentage.Load())
			return c.JSON(api.HealthResponse{
				Mode:     "restore",
				Progress: &pct,
			})
		}
	}

	portsOpen := p.portService.MandatoryPortsOpen()

	if !p.timeoutDone && !portsOpen {
		c.SendStatus(503)
		return c.JSON(api.HealthResponse{
			Mode: "manditory_ports",
		})

	}
	if p.Started == nil {
		return c.JSON(api.HealthResponse{
			Mode: "idle",
		})
	}

	return c.JSON(api.HealthResponse{
		Mode:      "ok",
		StartDate: p.Started,
	})
}

func (p *HealthHandler) countdown(timeout *time.Timer) {
	<-timeout.C
	p.timeoutDone = true
}
