package middlewares

import (
	"github.com/gofiber/fiber/v2"
	constants "github.com/highcard-dev/daemon/internal"
)

func NewHeaderMiddleware() fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		ctx.Response().Header.Set("Druid-Version", constants.Version)
		return ctx.Next()
	}
}
