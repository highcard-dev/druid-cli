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

// @Summary Serve public UI content
// @ID getPublicUI
// @Tags ui, druid, daemon
// @Accept */*
// @Produce html
// @Success 200
// @Router /public [get]
func (uh *UiHandler) Public(ctx *fiber.Ctx) error {
	// Extract the file path from the route parameter
	// The route is /public/* so we need to get everything after /public/
	requestPath := ctx.Params("*")

	// If no specific file is requested, serve index
	if requestPath == "" {
		return uh.PublicIndex(ctx)
	}

	// Serve the requested file from scrollDir/public/<file>
	content, contentType, err := uh.uiService.ServeFile("public", requestPath)
	if err != nil {
		return ctx.Status(404).SendString("File not found")
	}

	ctx.Set("Content-Type", contentType)
	return ctx.Send(content)
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

// @Summary Serve private UI content
// @ID getPrivateUI
// @Tags ui, druid, daemon
// @Accept */*
// @Produce html
// @Success 200
// @Router /private [get]
func (uh *UiHandler) Private(ctx *fiber.Ctx) error {
	// Extract the file path from the route parameter
	// The route is /private/* so we need to get everything after /private/
	requestPath := ctx.Params("*")

	// If no specific file is requested, serve index
	if requestPath == "" {
		return uh.PrivateIndex(ctx)
	}

	// Serve the requested file from scrollDir/private/<file>
	content, contentType, err := uh.uiService.ServeFile("private", requestPath)
	if err != nil {
		return ctx.Status(404).SendString("File not found")
	}

	ctx.Set("Content-Type", contentType)
	return ctx.Send(content)
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
