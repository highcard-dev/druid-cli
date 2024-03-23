package middlewares

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type AuthenticationOptions struct {
	ValidateQuery            bool
	FallbackHeaderValidation bool
}

func TokenAuthentication(authorizerService ports.AuthorizerServiceInterface) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		token := ctx.Query("token")
		if token != "" {
			_, authQueryError := authorizerService.CheckQuery(token)
			if authQueryError != nil {
				logger.Log().Error("Token Authentication failed",
					zap.String(logger.LogKeyContext, logger.LogContextHttp),
					zap.String("type", "query"),
					zap.Error(authQueryError),
				)
				return errors.New("401 - Your spell has no permission to cast that magic!")
			} else {
				return ctx.Next()
			}
		}
		// Get the Token Authentication credentials from header
		if _, authHeaderError := authorizerService.CheckHeader(ctx); authHeaderError != nil {
			logger.Log().Error("Token Authentication failed",
				zap.String(logger.LogKeyContext, logger.LogContextHttp),
				zap.String("type", "header"),
				zap.Error(authHeaderError),
			)
			return errors.New("401 - Your spell has no permission to cast that magic!")
		}
		return ctx.Next()
	}
}

func NewUserInjector() fiber.Handler {
	return func(ctx *fiber.Ctx) error {

		user := ctx.Locals("user").(*jwt.Token)

		userId, ok := user.Claims.(jwt.MapClaims)["sub"]
		if !ok {
			return fiber.NewError(fiber.StatusBadRequest, "Invalid user id in jwt sub field")
		}
		ctx.Context().SetUserValue("userID", userId)
		return ctx.Next()
	}
}
