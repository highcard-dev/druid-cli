package handlers

import (
	"context"
	"io"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type WebsocketHandler struct {
	scrolls                    *ScrollHandler
	authorizer                 ports.AuthorizerServiceInterface
	allowUnauthenticatedPublic bool
}

func NewWebsocketHandler() *WebsocketHandler {
	return &WebsocketHandler{}
}

func (h *WebsocketHandler) SetScrollHandler(scrolls *ScrollHandler) {
	h.scrolls = scrolls
}

func (h *WebsocketHandler) SetAuthorizer(authorizer ports.AuthorizerServiceInterface) {
	h.authorizer = authorizer
}

func (h *WebsocketHandler) SetAllowUnauthenticatedPublic(allow bool) {
	h.allowUnauthenticatedPublic = allow
}

func (h *WebsocketHandler) AttachConsole(c *websocket.Conn) {
	h.attach(c, c.Params("id"), c.Params("console"))
}

func (h *WebsocketHandler) AttachScrollConsole(c *websocket.Conn) {
	if !h.PublicQueryAuth(c) {
		_ = c.Close()
		return
	}
	h.AttachConsole(c)
}

func (h *WebsocketHandler) attach(c *websocket.Conn, runtimeID string, consoleID string) {
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := h.scrolls.supervisor.OpenConsole(ctx, runtimeID, consoleID)
	if err != nil {
		logger.Log().Warn("Console unavailable", zap.String("runtime", runtimeID), zap.String("console", consoleID), zap.Error(err))
		return
	}
	defer session.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			if _, err := session.Write(data); err != nil {
				logger.Log().Debug("Failed to write console input", zap.Error(err))
				return
			}
		}
	}()
	output := make(chan []byte, 25)
	go func() {
		defer close(output)
		buffer := make([]byte, 32*1024)
		for {
			n, err := session.Read(buffer)
			if n > 0 {
				select {
				case output <- append([]byte(nil), buffer[:n]...):
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					logger.Log().Debug("Console output ended", zap.Error(err))
				}
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
		case data, ok := <-output:
			if !ok {
				return
			}
			if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-pingTicker.C:
			if err := c.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
