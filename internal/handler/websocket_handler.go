package handler

import (
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
	subscriptionChannel := wh.consoleService.GetSubscription(param)

	if subscriptionChannel == nil {
		c.Close()
		return
	}

	//fetch channel and send to websocket
	for {
		//send 1024 bytes at a time
		buffer := <-subscriptionChannel
		//if nil is send, assume the channel is closed
		if buffer == nil {
			c.Close()
			return
		}
		err := c.WriteMessage(websocket.TextMessage, *buffer)
		if err != nil {
			wh.consoleService.DeleteSubscription(param, subscriptionChannel)
			c.Close()
			return
		}
	}
}
