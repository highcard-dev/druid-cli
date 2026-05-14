package handlers

import (
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type WebsocketHandler struct {
	consoleService *services.ConsoleManager
	scrolls        *ScrollHandler
	authorizer     ports.AuthorizerServiceInterface
}

func NewWebsocketHandler(consoleService *services.ConsoleManager) *WebsocketHandler {
	return &WebsocketHandler{consoleService: consoleService}
}

func (h *WebsocketHandler) SetScrollHandler(scrolls *ScrollHandler) {
	h.scrolls = scrolls
}

func (h *WebsocketHandler) SetAuthorizer(authorizer ports.AuthorizerServiceInterface) {
	h.authorizer = authorizer
}

func (h *WebsocketHandler) AttachConsole(c *websocket.Conn) {
	consoleID := c.Params("console")
	if id := c.Params("id"); id != "" {
		consoleID = id + "/" + consoleID
	}
	h.attach(c, consoleID)
}

func (h *WebsocketHandler) AttachScrollConsole(c *websocket.Conn) {
	if !h.PublicQueryAuth(c) {
		_ = c.Close()
		return
	}
	h.AttachConsole(c)
}

func (h *WebsocketHandler) attach(c *websocket.Conn, consoleID string) {
	defer c.Close()

	console := h.consoleService.GetConsole(consoleID)
	if console == nil {
		logger.Log().Warn("Console not found", zap.String("console", consoleID))
		return
	}

	subscription := console.Channel.Subscribe()
	defer console.Channel.Unsubscribe(subscription)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			if console.WriteInput != nil {
				if err := console.WriteInput(string(data)); err != nil {
					logger.Log().Debug("Failed to write console input", zap.Error(err))
					return
				}
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
