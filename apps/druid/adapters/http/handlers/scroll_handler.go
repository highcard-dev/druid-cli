package handlers

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/core/services"
)

type ScrollHandler struct {
	supervisor     *appservices.RuntimeSupervisor
	consoleService *services.ConsoleManager
	logService     *services.LogManager
	authorizer     ports.AuthorizerServiceInterface
}

func NewScrollHandler(supervisor *appservices.RuntimeSupervisor, consoleService *services.ConsoleManager, logService *services.LogManager, authorizer ...ports.AuthorizerServiceInterface) *ScrollHandler {
	var auth ports.AuthorizerServiceInterface
	if len(authorizer) > 0 {
		auth = authorizer[0]
	}
	return &ScrollHandler{
		supervisor:     supervisor,
		consoleService: consoleService,
		logService:     logService,
		authorizer:     auth,
	}
}

func registryCredentials(in *[]api.RegistryCredential) []domain.RegistryCredential {
	if in == nil || len(*in) == 0 {
		return nil
	}
	out := make([]domain.RegistryCredential, 0, len(*in))
	for _, credential := range *in {
		out = append(out, domain.RegistryCredential{
			Host:     credential.Host,
			Username: credential.Username,
			Password: credential.Password,
		})
	}
	return out
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
	ownerID := ""
	if request.OwnerId != nil {
		ownerID = *request.OwnerId
	}
	runtimeScroll, err := h.supervisor.CreateWithOwner(request.Artifact, name, ownerID, registryCredentials(request.RegistryCredentials))
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

func (h *ScrollHandler) EnsureScroll(c *fiber.Ctx) error {
	var request api.EnsureScrollRequest
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	name := ""
	if request.Name != nil && *request.Name != "" {
		name = *request.Name
	} else if request.Id != nil && *request.Id != "" {
		name = *request.Id
	}
	ownerID := ""
	if request.OwnerId != nil {
		ownerID = *request.OwnerId
	}
	runtimeScroll, err := h.supervisor.EnsureWithOwner(request.Artifact, name, ownerID, registryCredentials(request.RegistryCredentials))
	if err != nil {
		if errors.Is(err, appservices.ErrRuntimeMaterializationUnsupported) {
			return fiber.NewError(fiber.StatusNotImplemented, err.Error())
		}
		return err
	}
	return c.JSON(runtimeScroll)
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
	if err := h.supervisor.DeleteWithPolicy(id, c.QueryBool("purge_data", false)); err != nil {
		return err
	}
	return c.JSON(api.DeletedScroll{
		Id:     runtimeScroll.ID,
		Status: "deleted",
	})
}

func (h *ScrollHandler) StartScroll(c *fiber.Ctx, id string) error {
	if _, err := h.getScroll(id); err != nil {
		return err
	}
	runtimeScroll, err := h.supervisor.StartScroll(id)
	if err != nil {
		return err
	}
	return c.JSON(runtimeScroll)
}

func (h *ScrollHandler) StopScroll(c *fiber.Ctx, id string) error {
	if _, err := h.getScroll(id); err != nil {
		return err
	}
	runtimeScroll, err := h.supervisor.Stop(id)
	if err != nil {
		return err
	}
	return c.JSON(runtimeScroll)
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

func (h *ScrollHandler) GetScrollConfig(c *fiber.Ctx, id string) error {
	if _, err := h.getScroll(id); err != nil {
		return err
	}
	scrollFile, err := h.supervisor.ScrollFile(id)
	if err != nil {
		return err
	}
	return c.JSON(scrollFile)
}

func (h *ScrollHandler) GetScrollQueue(c *fiber.Ctx, id string) error {
	if _, err := h.getScroll(id); err != nil {
		return err
	}
	queue, err := h.supervisor.Queue(id)
	if err != nil {
		return err
	}
	return c.JSON(queue)
}

func (h *ScrollHandler) GetScrollProcedures(c *fiber.Ctx, id string) error {
	if _, err := h.getScroll(id); err != nil {
		return err
	}
	procedures, err := h.supervisor.Procedures(id)
	if err != nil {
		return err
	}
	return c.JSON(procedures)
}

func (h *ScrollHandler) GetScrollConsoles(c *fiber.Ctx, id string) error {
	if _, err := h.getScroll(id); err != nil {
		return err
	}
	prefix := id + "/"
	consoles := map[string]*domain.Console{}
	for consoleID, console := range h.consoleService.GetConsoles() {
		if strings.HasPrefix(consoleID, prefix) {
			consoles[strings.TrimPrefix(consoleID, prefix)] = console
		}
	}
	return c.JSON(consoles)
}

func (h *ScrollHandler) GetScrollLogs(c *fiber.Ctx, id string) error {
	logs, err := h.scrollLogs(id)
	if err != nil {
		return err
	}
	return c.JSON(logs)
}

func (h *ScrollHandler) GetDaemonScroll(c *fiber.Ctx) error {
	return h.GetScrollConfig(c, c.Params("id"))
}

func (h *ScrollHandler) RunDaemonCommand(c *fiber.Ctx) error {
	var request struct {
		Command string `json:"command"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if request.Command == "" {
		return fiber.NewError(fiber.StatusBadRequest, "command is required")
	}
	if _, err := h.supervisor.Run(c.Params("id"), request.Command); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusOK)
}

func (h *ScrollHandler) GetDaemonQueue(c *fiber.Ctx) error {
	return h.GetScrollQueue(c, c.Params("id"))
}

func (h *ScrollHandler) GetDaemonProcedures(c *fiber.Ctx) error {
	return h.GetScrollProcedures(c, c.Params("id"))
}

func (h *ScrollHandler) GetDaemonConsoles(c *fiber.Ctx) error {
	return h.GetScrollConsoles(c, c.Params("id"))
}

func (h *ScrollHandler) GetDaemonLogs(c *fiber.Ctx) error {
	logs, err := h.scrollLogs(c.Params("id"))
	if err != nil {
		return err
	}
	streams := make([]map[string]any, 0, len(logs))
	for stream, log := range logs {
		streams = append(streams, map[string]any{"stream": stream, "log": log})
	}
	return c.JSON(streams)
}

func (h *ScrollHandler) GetDaemonStreamLogs(c *fiber.Ctx) error {
	logs, err := h.scrollLogs(c.Params("id"))
	if err != nil {
		return err
	}
	stream := c.Params("stream")
	return c.JSON(map[string]any{"stream": stream, "log": logs[stream]})
}

func (h *ScrollHandler) GetDaemonPorts(c *fiber.Ctx) error {
	return h.GetScrollPorts(c, c.Params("id"))
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

func (h *ScrollHandler) GetScrollRoutingTargets(c *fiber.Ctx, id string) error {
	if _, err := h.getScroll(id); err != nil {
		return err
	}
	targets, err := h.supervisor.RoutingTargets(id)
	if err != nil {
		return err
	}
	return c.JSON(targets)
}

func (h *ScrollHandler) ApplyScrollRouting(c *fiber.Ctx, id string) error {
	if _, err := h.getScroll(id); err != nil {
		return err
	}
	var request struct {
		Assignments []domain.RuntimeRouteAssignment `json:"assignments"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	runtimeScroll, err := h.supervisor.ApplyRouting(id, request.Assignments)
	if err != nil {
		return err
	}
	return c.JSON(runtimeScroll)
}

func (h *ScrollHandler) BackupScroll(c *fiber.Ctx, id string) error {
	if _, err := h.getScroll(id); err != nil {
		return err
	}
	var request api.RuntimeArtifactOperationRequest
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	runtimeScroll, err := h.supervisor.Backup(id, request.Artifact, registryCredentials(request.RegistryCredentials))
	if err != nil {
		return err
	}
	return c.JSON(runtimeScroll)
}

func (h *ScrollHandler) RestoreScroll(c *fiber.Ctx, id string) error {
	if _, err := h.getScroll(id); err != nil {
		return err
	}
	var request api.RuntimeArtifactOperationRequest
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	restart := false
	if request.Restart != nil {
		restart = *request.Restart
	}
	runtimeScroll, err := h.supervisor.Restore(id, request.Artifact, restart, registryCredentials(request.RegistryCredentials))
	if err != nil {
		return err
	}
	return c.JSON(runtimeScroll)
}

func (h *ScrollHandler) getScroll(id string) (*domain.RuntimeScroll, error) {
	runtimeScroll, err := h.supervisor.Get(id)
	if errors.Is(err, services.ErrScrollNotFound) {
		return nil, fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return runtimeScroll, err
}

func (h *ScrollHandler) scrollLogs(id string) (map[string][]string, error) {
	if _, err := h.getScroll(id); err != nil {
		return nil, err
	}
	prefix := id + "/"
	logs := map[string][]string{}
	for streamID, log := range h.logService.GetStreams() {
		if !strings.HasPrefix(streamID, prefix) {
			continue
		}
		response := make(chan []byte, 100)
		log.Req <- response
		lines := []string{}
		for line := range response {
			lines = append(lines, string(line))
		}
		logs[strings.TrimPrefix(streamID, prefix)] = lines
	}
	return logs, nil
}
