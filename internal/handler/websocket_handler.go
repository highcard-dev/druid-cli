package handler

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/core/services"
)

type WebsocketHandler struct {
	authorizerService ports.AuthorizerServiceInterface
	scrollService     ports.ScrollServiceInterface
	hub               *services.WebsocketBroadcaster
}

type TokenHttpResponse struct {
	Token string `json:"token" validate:"required"`
} // @name WebsocketToken

func NewWebsocketHandler(
	authorizerService ports.AuthorizerServiceInterface,
	scrollService ports.ScrollServiceInterface,
	hub *services.WebsocketBroadcaster,
) *WebsocketHandler {
	return &WebsocketHandler{
		authorizerService,
		scrollService,
		hub,
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

func (wh WebsocketHandler) HandleProcess(c *websocket.Conn) {

	client := &services.WebsocketClient{
		Conn: c,
		Send: make(chan []byte, 256),
		Hub:  *wh.hub,
	}
	client.Pump()
}
