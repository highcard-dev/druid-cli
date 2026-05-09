package handlers

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services"
)

type ScrollHandler struct {
	supervisor *appservices.RuntimeSupervisor
}

func NewScrollHandler(supervisor *appservices.RuntimeSupervisor) *ScrollHandler {
	return &ScrollHandler{
		supervisor: supervisor,
	}
}

func (h *ScrollHandler) ListScrolls(c *fiber.Ctx) error {
	scrolls, err := h.supervisor.List()
	if err != nil {
		return err
	}
	return c.JSON(scrolls)
}

func (h *ScrollHandler) CreateScroll(c *fiber.Ctx) error {
	var request api.CreateScrollRequest
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	name := ""
	if request.Name != nil && *request.Name != "" {
		name = *request.Name
	} else if request.Id != nil && *request.Id != "" {
		name = *request.Id
	}
	scrollRoot := ""
	if request.ScrollRoot != nil {
		scrollRoot = *request.ScrollRoot
	}
	dataRoot := ""
	if request.DataRoot != nil {
		dataRoot = *request.DataRoot
	}
	runtimeScroll, err := h.supervisor.Create(request.Artifact, name, scrollRoot, dataRoot)
	if err != nil {
		if errors.Is(err, services.ErrScrollAlreadyExists) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		if errors.Is(err, appservices.ErrRuntimeMaterializationUnsupported) {
			return fiber.NewError(fiber.StatusNotImplemented, err.Error())
		}
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(runtimeScroll)
}

func (h *ScrollHandler) GetScroll(c *fiber.Ctx, id string) error {
	runtimeScroll, err := h.getScroll(id)
	if err != nil {
		return err
	}
	return c.JSON(runtimeScroll)
}

func (h *ScrollHandler) DeleteScroll(c *fiber.Ctx, id string) error {
	runtimeScroll, err := h.getScroll(id)
	if err != nil {
		return err
	}
	if err := h.supervisor.Delete(id); err != nil {
		return err
	}
	return c.JSON(api.DeletedScroll{
		Id:     runtimeScroll.ID,
		Status: "deleted",
	})
}

func (h *ScrollHandler) RunScrollCommand(c *fiber.Ctx, id string, command string) error {
	runtimeScroll, err := h.getScroll(id)
	if err != nil {
		return err
	}
	updated, err := h.supervisor.Run(runtimeScroll.ID, command)
	if err != nil {
		return err
	}
	return c.JSON(updated)
}

func (h *ScrollHandler) GetScrollPorts(c *fiber.Ctx, id string) error {
	runtimeScroll, err := h.getScroll(id)
	if err != nil {
		return err
	}
	statuses, err := h.supervisor.Ports(runtimeScroll.ID)
	if err != nil {
		return err
	}
	return c.JSON(statuses)
}

func (h *ScrollHandler) getScroll(id string) (*domain.RuntimeScroll, error) {
	runtimeScroll, err := h.supervisor.Get(id)
	if errors.Is(err, services.ErrScrollNotFound) {
		return nil, fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return runtimeScroll, err
}
