package handler

import (
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
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

type TokenHttpResponse struct {
	Token string `json:"token" validate:"required"`
} // @name WebsocketToken

type ConsolesHttpResponse struct {
	Consoles map[string]*domain.Console `json:"consoles" validate:"required"`
} // @name ConsolesResponse

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

// @Summary Get current scroll
// @Description Get the metrics for all processes.
// @ID createToken
// @Tags websocket, druid, daemon
// @Accept json
// @Produce json
// @Success 200 {object} TokenHttpResponse
// @Router /api/v1/token [get]
func (ah WebsocketHandler) CreateToken(c *fiber.Ctx) error {
	token := ah.authorizerService.GenerateQueryToken()

	c.JSON(TokenHttpResponse{Token: token})
	return nil
}

// @Summary Get All Consoles
// @Description Get List of all consoles
// @ID getConsoles
// @Tags druid, daemon, console
// @Accept json
// @Produce json
// @Success 200 {object} ConsolesHttpResponse
// @Router /api/v1/consoles [get]
func (ah WebsocketHandler) Consoles(c *fiber.Ctx) error {
	consoles := ah.consoleService.GetConsoles()

	c.JSON(ConsolesHttpResponse{Consoles: consoles})
	return nil
}

func (wh WebsocketHandler) HandleProcess(c *websocket.Conn) {
	param := c.Params("console")

	// Create a done channel to signal when the connection should be closed
	done := make(chan struct{})

	ticker := time.NewTicker(pingPeriod)
	defer func() {
		close(done)
		ticker.Stop()
		c.Close()
	}()

	channel := wh.consoleService.GetConsole(param)
	if channel == nil {
		return
	}

	subscriptionChannel := channel.Channel.Subscribe()
	defer channel.Channel.Unsubscribe(subscriptionChannel)

	c.SetReadLimit(maxMessageSize)
	c.SetReadDeadline(time.Now().Add(pongWait))
	c.SetPongHandler(func(string) error { c.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	// Start a goroutine to read messages (to handle pong responses and detect broken connections)
	go func() {
		defer func() {
			select {
			case <-done:
				// Connection is already being closed
			default:
				close(done)
			}
		}()

		for {
			_, _, err := c.ReadMessage()
			if err != nil {
				logger.Log().Debug("WebSocket client disconnected", zap.Error(err))
				return
			}
		}
	}()

	//fetch channel and send to websocket
	for {
		select {
		case <-done:
			logger.Log().Debug("WebSocket connection done signal received")
			return

		//send 1024 bytes at a time
		case buffer, ok := <-subscriptionChannel:
			c.SetWriteDeadline(time.Now().Add(writeWait))
			//if nil is send, assume the channel is closed
			if buffer == nil || !ok {
				return
			}
			err := c.WriteMessage(websocket.TextMessage, *buffer)
			if err != nil {
				logger.Log().Debug("WebSocket client disconnected while sending message", zap.Error(err))
				return
			}
		case <-ticker.C:
			c.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.WriteMessage(websocket.PingMessage, nil); err != nil {
				logger.Log().Debug("WebSocket client disconnected during ping", zap.Error(err))
				return
			}
		}
	}
}
