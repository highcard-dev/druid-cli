package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type uiPublishUploadRequest struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type uiPublishFailureRequest struct {
	Error string `json:"error"`
}

func (h *ScrollHandler) ClaimUIPackagePublish(c *fiber.Ctx) error {
	identity, err := devWorkloadIdentity(c)
	if err != nil {
		return err
	}
	request, err := h.supervisor.ClaimUIPackagePublish(identity.RuntimeID, identity.PodUID)
	if err != nil {
		return err
	}
	if request == nil {
		return c.SendStatus(fiber.StatusNoContent)
	}
	return c.JSON(request)
}

func (h *ScrollHandler) PrepareUIPackagePublish(c *fiber.Ctx) error {
	identity, err := devWorkloadIdentity(c)
	if err != nil {
		return err
	}
	var request uiPublishUploadRequest
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	url, err := h.supervisor.PrepareUIPackagePublish(identity.RuntimeID, c.Params("requestID"), identity.PodUID, request.SHA256, request.Size)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"url": url})
}

func (h *ScrollHandler) CompleteUIPackagePublish(c *fiber.Ctx) error {
	identity, err := devWorkloadIdentity(c)
	if err != nil {
		return err
	}
	var request uiPublishUploadRequest
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	runtimeScroll, err := h.supervisor.CompleteUIPackagePublish(identity.RuntimeID, c.Params("requestID"), identity.PodUID, request.SHA256, request.Size)
	if err != nil {
		return err
	}
	return c.JSON(runtimeScroll)
}

func (h *ScrollHandler) FailUIPackagePublish(c *fiber.Ctx) error {
	identity, err := devWorkloadIdentity(c)
	if err != nil {
		return err
	}
	var request uiPublishFailureRequest
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := h.supervisor.FailUIPackagePublish(identity.RuntimeID, c.Params("requestID"), identity.PodUID, strings.TrimSpace(request.Error)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func devWorkloadIdentity(c *fiber.Ctx) (ports.RuntimeWorkloadIdentity, error) {
	identity, ok := c.Locals("druid-workload-identity").(ports.RuntimeWorkloadIdentity)
	if !ok || identity.Kind != "dev" || identity.RuntimeID == "" || identity.PodUID == "" {
		return ports.RuntimeWorkloadIdentity{}, fiber.NewError(fiber.StatusForbidden, "Druid dev workload identity is required")
	}
	return identity, nil
}
