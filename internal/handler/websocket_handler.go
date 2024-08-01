package handler

import (
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
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

	ticker := time.NewTicker(pingPeriod)
	defer func() {
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

	//fetch channel and send to websocket
	for {
		select {
		//send 1024 bytes at a time
		case buffer, ok := <-subscriptionChannel:

			c.SetWriteDeadline(time.Now().Add(writeWait))
			//if nil is send, assume the channel is closed
			if buffer == nil || !ok {
				return
			}
			err := c.WriteMessage(websocket.TextMessage, *buffer)
			if err != nil {
				return
			}
		case <-ticker.C:
			c.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
