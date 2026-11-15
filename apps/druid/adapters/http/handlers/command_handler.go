package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/apps/druid/adapters/toolrequest"
	services "github.com/highcard-dev/daemon/apps/druid/core/services"
)

func (h *ScrollHandler) callCommandToolHTTP(c *fiber.Ctx, name string) error {
	var input services.ToolInput
	if len(c.Body()) > 0 {
		var err error
		input, err = toolrequest.Decode(c.Body())
		if err != nil {
			return fiber.NewError(400, "invalid tool request")
		}
	}
	input.Command = c.Params("command", input.Command)
	input.Console = c.Params("console", input.Console)
	input.Scope = c.Params("scope", input.Scope)
	if c.Method() == "GET" {
		input.TailLines = c.QueryInt("tail_lines", 0)
		input.TimeoutSeconds = c.QueryInt("timeout_seconds", 0)
	}
	result, err := h.supervisor.CallCommandTool(c.UserContext(), toolrequest.Bearer(c.Get("Authorization")), c.Params("id"), name, input)
	if err != nil {
		return fiber.NewError(toolrequest.Status(err), toolrequest.Message(err))
	}
	return c.JSON(result)
}

func (h *ScrollHandler) GetDaemonCommands(c *fiber.Ctx) error {
	return h.callCommandToolHTTP(c, "list_commands")
}

func (h *ScrollHandler) CreateDaemonCommand(c *fiber.Ctx) error {
	return h.callCommandToolHTTP(c, "create_command")
}

func (h *ScrollHandler) WaitDaemonCommand(c *fiber.Ctx) error {
	return h.callCommandToolHTTP(c, "wait_for_command")
}

func (h *ScrollHandler) GetWorkloadQueue(c *fiber.Ctx) error {
	return h.callCommandToolHTTP(c, "get_queue")
}

func (h *ScrollHandler) RunWorkloadCommand(c *fiber.Ctx) error {
	return h.callCommandToolHTTP(c, "run_command")
}

func (h *ScrollHandler) RemoveWorkloadCommand(c *fiber.Ctx) error {
	return h.callCommandToolHTTP(c, "remove_command")
}

func (h *ScrollHandler) PublishWorkloadUIPackage(c *fiber.Ctx) error {
	return h.callCommandToolHTTP(c, "publish_ui")
}

func (h *ScrollHandler) GetDaemonStatus(c *fiber.Ctx) error {
	return h.callCommandToolHTTP(c, "get_deployment_status")
}
