package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
)

func (h *ScrollHandler) CreateDaemonToken(c *fiber.Ctx) error {
	if h == nil || h.authorizer == nil {
		return c.JSON(map[string]string{"token": ""})
	}
	runtimeScroll, err := h.getScroll(c.Params("id"))
	if err != nil {
		return err
	}
	ownerID := runtimeScroll.OwnerID
	if subject, ok := c.Locals(ownerLocal).(string); ok && subject != "" {
		ownerID = subject
	}
	if h.authorizer == nil {
		return c.JSON(map[string]string{"token": ""})
	}
	return c.JSON(map[string]string{"token": h.authorizer.GenerateQueryToken(runtimeScroll.ID, ownerID)})
}

func (h *ScrollHandler) AddDaemonCommand(c *fiber.Ctx) error {
	var request domain.CommandInstructionSet
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := h.supervisor.AddCommand(c.Params("id"), c.Params("command"), &request); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *ScrollHandler) RemoveDaemonCommand(c *fiber.Ctx) error {
	if err := h.supervisor.RemoveCommand(c.Params("id"), c.Params("command")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
