package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type AnnotationHandler struct {
	scrollService ports.ScrollServiceInterface
}

func NewAnnotationHandler(scrollService ports.ScrollServiceInterface) *AnnotationHandler {
	return &AnnotationHandler{
		scrollService: scrollService,
	}
}

func (ah AnnotationHandler) Annotations(c *fiber.Ctx) error {
	annotationsFile := ah.scrollService.GetDir() + "/annotations.json"
	return c.SendFile(annotationsFile)
}
