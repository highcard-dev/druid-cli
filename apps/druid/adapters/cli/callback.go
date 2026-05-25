package cli

import (
	"github.com/gofiber/fiber/v2"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/callbackapi"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type runtimeCallbackHandler struct {
	callbacks *appservices.WorkerCallbackManager
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
	if err := h.callbacks.Complete(string(runtimeID), result.Token, runtimeResult); err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}
