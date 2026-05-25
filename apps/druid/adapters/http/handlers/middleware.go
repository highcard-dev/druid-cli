package handlers

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func ErrorHandler(c *fiber.Ctx, err error) error {
	status := fiber.StatusInternalServerError
	if fiberErr, ok := err.(*fiber.Error); ok {
		status = fiberErr.Code
	}
	if status >= fiber.StatusInternalServerError {
		logger.Log().Error("HTTP request failed",
			zap.String("method", c.Method()),
			zap.String("path", c.Path()),
			zap.Int("status", status),
			zap.Error(err),
		)
	}
	return fiber.DefaultErrorHandler(c, err)
}

func RequestLogger(c *fiber.Ctx) error {
	start := time.Now()
	err := c.Next()
	status := c.Response().StatusCode()
	if err != nil {
		status = fiber.StatusInternalServerError
		if fiberErr, ok := err.(*fiber.Error); ok {
			status = fiberErr.Code
		}
	}
	logger.Log().Debug("HTTP request",
		zap.String("method", c.Method()),
		zap.String("path", c.Path()),
		zap.Int("status", status),
		zap.Duration("duration", time.Since(start)),
		zap.String("ip", c.IP()),
		zap.Error(err),
	)
	return err
}
