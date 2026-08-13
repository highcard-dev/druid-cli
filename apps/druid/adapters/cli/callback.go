package cli

import (
	"github.com/gofiber/fiber/v2"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/callbackapi"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type runtimeCallbackHandler struct {
	callbacks            *appservices.WorkerCallbackManager
	allowUnauthenticated bool
}

func (h runtimeCallbackHandler) ReportProgress(c *fiber.Ctx) error {
	var report struct {
		Token      string `json:"token"`
		Percentage *int64 `json:"percentage"`
	}
	if err := c.BodyParser(&report); err != nil || report.Percentage == nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid progress report")
	}
	if *report.Percentage < 0 || *report.Percentage > 100 {
		return fiber.NewError(fiber.StatusBadRequest, "percentage must be between 0 and 100")
	}
	if err := h.callbacks.ReportProgress(c.Params("runtime_id"), report.Token, *report.Percentage); err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h runtimeCallbackHandler) CompleteWorker(c *fiber.Ctx, runtimeID callbackapi.Runtime) error {
	var result callbackapi.WorkerResult
	if err := c.BodyParser(&result); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	runtimeResult := ports.RuntimeWorkerResult{}
	if result.ScrollYaml != nil {
		runtimeResult.ScrollYAML = *result.ScrollYaml
	}
	if result.ArtifactDigest != nil {
		runtimeResult.ArtifactDigest = *result.ArtifactDigest
	}
	if result.Error != nil {
		runtimeResult.Error = *result.Error
	}
	if !h.allowUnauthenticated {
		identity, ok := c.Locals("druid-workload-identity").(ports.RuntimeWorkloadIdentity)
		if !ok || identity.Kind != "worker" || identity.RuntimeID != string(runtimeID) {
			return fiber.NewError(fiber.StatusForbidden, "worker identity does not match runtime")
		}
	}
	if err := h.callbacks.Complete(string(runtimeID), runtimeResult); err != nil {
		return fiber.NewError(fiber.StatusConflict, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}
