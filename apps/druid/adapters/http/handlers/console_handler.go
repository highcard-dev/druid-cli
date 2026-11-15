package handlers

import (
	"github.com/gofiber/fiber/v2"
)

func (h *ScrollHandler) ReadDaemonLogs(c *fiber.Ctx) error {
	return h.callCommandToolHTTP(c, "read_logs")
}

func (h *ScrollHandler) SearchDaemonLogs(c *fiber.Ctx) error {
	return h.callCommandToolHTTP(c, "search_logs")
}

func (h *ScrollHandler) WriteDaemonStdin(c *fiber.Ctx) error {
	return h.callCommandToolHTTP(c, "send_stdin")
}
