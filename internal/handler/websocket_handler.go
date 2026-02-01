package handler

import (
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type WebsocketHandler struct {
	authorizerService ports.AuthorizerServiceInterface
	scrollService     ports.ScrollServiceInterface
	consoleService    ports.ConsoleManagerInterface
}

func domainConsoleToAPI(dc *domain.Console) api.Console {
	return api.Console{
		Type:      api.ConsoleType(dc.Type),
		InputMode: dc.InputMode,
		Exit:      dc.Exit,
	}
}

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

func NewWebsocketHandler(
	authorizerService ports.AuthorizerServiceInterface,
	scrollService ports.ScrollServiceInterface,
	consoleService ports.ConsoleManagerInterface,
) *WebsocketHandler {
	return &WebsocketHandler{
		authorizerService,
		scrollService,
		consoleService,
	}
}

func (ah WebsocketHandler) CreateToken(c *fiber.Ctx) error {
	token := ah.authorizerService.GenerateQueryToken()

	c.JSON(api.TokenResponse{Token: token})
	return nil
}

func (ah WebsocketHandler) GetConsoles(c *fiber.Ctx) error {
	consoles := ah.consoleService.GetConsoles()

	// Convert domain consoles to API consoles
	apiConsoles := make(map[string]api.Console, len(consoles))
	for k, v := range consoles {
		apiConsoles[k] = domainConsoleToAPI(v)
	}

	c.JSON(api.ConsolesResponse{Consoles: apiConsoles})
	return nil
}

func (wh WebsocketHandler) HandleProcess(c *websocket.Conn) {
	param := c.Params("console")
	defer c.Close()

	// Get console channel
	channel := wh.consoleService.GetConsole(param)
	if channel == nil {
		logger.Log().Warn("Console not found", zap.String("console", param))
		return
	}

	// Subscribe to console output
	subscriptionChannel := channel.Channel.Subscribe()
	defer channel.Channel.Unsubscribe(subscriptionChannel)

	// Set up ping/pong
	c.SetReadLimit(maxMessageSize)
	c.SetReadDeadline(time.Now().Add(pongWait))
	c.SetPongHandler(func(string) error {
		c.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	logger.Log().Info("WebSocket client connected to console", zap.String("console", param))

	// Create ping ticker
	pingTicker := time.NewTicker(pingPeriod)
	defer pingTicker.Stop()

	// Start reader goroutine to detect disconnects
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, _, err := c.ReadMessage()
			if err != nil {
				logger.Log().Debug("WebSocket client disconnected", zap.Error(err))
				return
			}
		}
	}()

	// Main event loop
	for {
		select {
		case <-done:
			return

		case buffer, ok := <-subscriptionChannel:
			if buffer == nil || !ok {
				return
			}

			c.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.WriteMessage(websocket.TextMessage, *buffer); err != nil {
				logger.Log().Debug("Failed to send console output, client disconnected", zap.Error(err))
				return
			}

		case <-pingTicker.C:
			c.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.WriteMessage(websocket.PingMessage, nil); err != nil {
				logger.Log().Debug("Failed to send ping, client disconnected", zap.Error(err))
				return
			}
		}
	}
}
