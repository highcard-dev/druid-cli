package handlers

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

const ownerLocal = "druid-owner-id"

func (h *ScrollHandler) PublicAuth(c *fiber.Ctx) error {
	if h.authorizer == nil {
		return c.Next()
	}
	auth, err := h.authorizer.CheckHeader(c)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, err.Error())
	}
	if auth == nil {
		return c.Next()
	}
	c.Locals(ownerLocal, auth.Subject)
	id := c.Params("id")
	if id == "" {
		return c.Next()
	}
	if err := h.authorizeRuntimeOwner(id, auth.Subject); err != nil {
		return err
	}
	return c.Next()
}

func (h *ScrollHandler) authorizeRuntimeOwner(id string, subject string) error {
	if subject == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "missing subject")
	}
	runtimeScroll, err := h.supervisor.Get(id)
	if err != nil {
		return err
	}
	if runtimeScroll.OwnerID != "" && runtimeScroll.OwnerID != subject {
		return fiber.NewError(fiber.StatusForbidden, "runtime owner mismatch")
	}
	return nil
}

func (h *WebsocketHandler) PublicQueryAuth(c *websocket.Conn) bool {
	if h.authorizer == nil {
		return true
	}
	if _, err := h.authorizer.CheckQuery(c.Params("id"), c.Query("token")); err != nil {
		return false
	}
	return true
}

type jwksProvider interface {
	JWKS() map[string]any
}

func RuntimeJWKS(authorizer ports.AuthorizerServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if authorizer == nil {
			return c.JSON(map[string]any{"keys": []any{}})
		}
		if provider, ok := authorizer.(jwksProvider); ok {
			return c.JSON(provider.JWKS())
		}
		return c.JSON(map[string]any{"keys": []any{}})
	}
}
