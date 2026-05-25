package handlers

import (
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
)

func (h *ScrollHandler) CreateDaemonToken(c *fiber.Ctx) error {
	if h == nil || h.authorizer == nil {
		return c.JSON(map[string]string{"token": ""})
	}
	runtimeScroll, err := h.getScroll(c.Params("id"))
	if err != nil {
		return err
	}
	ownerID := runtimeScroll.OwnerID
	if subject, ok := c.Locals(ownerLocal).(string); ok && subject != "" {
		ownerID = subject
	}
	if h.authorizer == nil {
		return c.JSON(map[string]string{"token": ""})
	}
	return c.JSON(map[string]string{"token": h.authorizer.GenerateQueryToken(runtimeScroll.ID, ownerID)})
}

func (h *ScrollHandler) AddDaemonCommand(c *fiber.Ctx) error {
	var request domain.CommandInstructionSet
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := h.supervisor.AddCommand(c.Params("id"), c.Params("command"), &request); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *ScrollHandler) GetDaemonWatchStatus(c *fiber.Ctx) error {
	status, err := h.supervisor.DevWatchStatus(c.Params("id"))
	if err != nil {
		return err
	}
	return c.JSON(status)
}

func (h *ScrollHandler) EnableDaemonWatch(c *fiber.Ctx) error {
	var request appservices.DevWatchRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&request); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	}
	status, err := h.supervisor.EnableDevWatch(c.Params("id"), request)
	if err != nil {
		return err
	}
	return c.JSON(api.DevWatchResponse{Status: "enabled", Enabled: status.Enabled, WatchedPaths: status.WatchedPaths})
}

func (h *ScrollHandler) DisableDaemonWatch(c *fiber.Ctx) error {
	status, err := h.supervisor.DisableDevWatch(c.Params("id"))
	if err != nil {
		return err
	}
	return c.JSON(api.DevWatchResponse{Status: "disabled", Enabled: status.Enabled, WatchedPaths: status.WatchedPaths})
}

func (h *WebsocketHandler) WatchNotifications(c *websocket.Conn) {
	defer c.Close()
	if h.scrolls == nil {
		return
	}
	subscription, unsubscribe, err := h.scrolls.supervisor.SubscribeDevWatch(c.Params("id"))
	if err != nil {
		return
	}
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}()

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()
	for {
		select {
		case <-done:
			return
		case data, ok := <-subscription:
			if !ok || data == nil {
				return
			}
			if err := c.WriteMessage(websocket.TextMessage, *data); err != nil {
				return
			}
		case <-pingTicker.C:
			if err := c.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *WebsocketHandler) WatchNotificationsPublic(c *websocket.Conn) {
	if !h.PublicQueryAuth(c) {
		_ = c.Close()
		return
	}
	h.WatchNotifications(c)
}
