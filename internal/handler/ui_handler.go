package handler

import (
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
// @Produce html
// @Success 200
// @Router /public/index [get]
func (uh *UiHandler) PublicIndex(ctx *fiber.Ctx) error {
	files, err := uh.uiService.GetIndex("public")
	if err != nil {
		return ctx.Status(500).SendString("Failed to retrieve public UI index")
	}
	return ctx.JSON(files)
}

// @Summary Serve private UI index
// @ID getPrivateUIIndex
// @Tags ui, druid, daemon
// @Accept */*
// @Produce html
// @Success 200
// @Router /private/index [get]
func (uh *UiHandler) PrivateIndex(ctx *fiber.Ctx) error {
	files, err := uh.uiService.GetIndex("private")
	if err != nil {
		return ctx.Status(500).SendString("Failed to retrieve private UI index")
	}
	return ctx.JSON(files)
}

// Ensure UiHandler implements UiHandlerInterface at compile time
var _ ports.UiHandlerInterface = (*UiHandler)(nil)
