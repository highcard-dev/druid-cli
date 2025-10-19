package handler

import (
	"errors"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type UiHandler struct {
	uiService ports.UiServiceInterface
}

func NewUiHandler(uiService ports.UiServiceInterface) *UiHandler {
	return &UiHandler{
		uiService: uiService,
	}
}

// @Summary Serve public UI index
// @ID getPublicUIIndex
// @Tags ui, druid, daemon
// @Accept */*
// @Produce json
// @Success 200 {array} string "List of files in public UI directory"
// @Failure 404 {object} map[string]string "Public UI directory not found"
// @Failure 500 {object} map[string]string "Internal server error with details"
// @Router /public/index [get]
func (uh *UiHandler) PublicIndex(ctx *fiber.Ctx) error {
	files, err := uh.uiService.GetIndex("public")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ctx.Status(fiber.StatusNotFound).SendString("Public UI directory not found")
		}
		return ctx.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	return ctx.JSON(files)
}

// @Summary Serve private UI index
// @ID getPrivateUIIndex
// @Tags ui, druid, daemon
// @Accept */*
// @Produce json
// @Success 200 {array} string "List of files in private UI directory"
// @Failure 404 {object} map[string]string "Private UI directory not found"
// @Failure 500 {object} map[string]string "Internal server error with details"
// @Router /private/index [get]
func (uh *UiHandler) PrivateIndex(ctx *fiber.Ctx) error {
	files, err := uh.uiService.GetIndex("private")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ctx.Status(fiber.StatusNotFound).SendString("Private UI directory not found")
		}
		return ctx.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	return ctx.JSON(files)
}

// Ensure UiHandler implements UiHandlerInterface at compile time
var _ ports.UiHandlerInterface = (*UiHandler)(nil)
