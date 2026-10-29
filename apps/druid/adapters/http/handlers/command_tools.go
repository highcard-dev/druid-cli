package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/google/jsonschema-go/jsonschema"
	services "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func commandToolStatus(err error) int {
	var failure *services.ToolError
	if errors.As(err, &failure) {
		return failure.Status
	}
	if errors.Is(err, workloadidentity.ErrUnauthorized) {
		return 401
	}
	if errors.Is(err, workloadidentity.ErrUnavailable) {
		return 503
	}
	return 500
}

func commandToolMessage(err error) string {
	if commandToolStatus(err) == 500 {
		return "runtime tool failed"
	}
	return workloadidentity.Redact(err.Error())
}

func bearer(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func commandToolInput(data []byte) (services.ToolInput, error) {
	var input services.ToolInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&input)
	return input, err
}

func (h *ScrollHandler) commandToolHTTP(name string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var input services.ToolInput
		if len(c.Body()) > 0 {
			var err error
			input, err = commandToolInput(c.Body())
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
		result, err := h.supervisor.CallCommandTool(c.UserContext(), bearer(c.Get("Authorization")), c.Params("id"), name, input)
		if err != nil {
			return fiber.NewError(commandToolStatus(err), commandToolMessage(err))
		}
		return c.JSON(result)
	}
}

func (h *ScrollHandler) registerCommandTools(app *fiber.App) {
	app.Get("/:id/api/v1/commands", h.commandToolHTTP("list_commands"))
	app.Post("/:id/api/v1/commands", h.commandToolHTTP("create_command"))
	app.Get("/:id/api/v1/commands/:command/wait", h.commandToolHTTP("wait_for_command"))
	app.Get("/:id/api/v1/consoles/:console/logs", h.commandToolHTTP("read_logs"))
	app.Post("/:id/api/v1/consoles/:console/logs/search", h.commandToolHTTP("search_logs"))
	app.Post("/:id/api/v1/consoles/:console/stdin", h.commandToolHTTP("send_stdin"))
	app.Get("/:id/api/v1/status", h.commandToolHTTP("get_deployment_status"))
	app.Get("/:id/api/v1/queue", h.ownerOrWorkload(h.commandToolHTTP("get_queue")), h.PublicAuth, h.GetDaemonQueue)
	app.Post("/:id/api/v1/command", h.ownerOrWorkload(h.commandToolHTTP("run_command")), h.PublicAuth, h.RunDaemonCommand)
	app.Delete("/:id/api/v1/scroll/commands/:command", h.ownerOrWorkload(h.commandToolHTTP("remove_command")), h.PublicAuth, h.RemoveDaemonCommand)
	app.Post("/:id/api/v1/ui/packages/:scope/publish", h.ownerOrWorkload(h.commandToolHTTP("publish_ui")), h.PublicAuth, h.PublishDaemonUIPackage)
	app.All("/:id/mcp", func(c *fiber.Ctx) error {
		deployment, token := c.Params("id"), bearer(c.Get("Authorization"))
		record, err := h.supervisor.AuthenticateCommand(c.UserContext(), token, deployment)
		if err != nil {
			return fiber.NewError(commandToolStatus(err), commandToolMessage(err))
		}
		handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
			server := mcp.NewServer(&mcp.Implementation{Name: "druid", Version: "1.0.0"}, nil)
			for _, definition := range commandToolDefinitions() {
				name := definition.Name
				if !record.Allows(services.ToolPermissions[name]) {
					continue
				}
				server.AddTool(definition, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					input, err := commandToolInput(request.Params.Arguments)
					if err != nil {
						return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "invalid tool request"}}}, nil
					}
					result, err := h.supervisor.CallCommandTool(ctx, token, deployment, name, input)
					if err != nil {
						return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: commandToolMessage(err)}}}, nil
					}
					data, err := json.Marshal(result)
					if err != nil {
						return nil, err
					}
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
				})
			}
			return server
		}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
		return adaptor.HTTPHandler(handler)(c)
	})
}

// OpenHands drops items/properties for nullable type arrays. Optional fields can
// be omitted instead; advertise concrete types without duplicating domain types.
func concreteToolSchema(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	if len(schema.Types) == 2 {
		for index, kind := range schema.Types {
			if kind == "null" {
				schema.Type = schema.Types[1-index]
				schema.Types = nil
				break
			}
		}
	}
	for _, property := range schema.Properties {
		concreteToolSchema(property)
	}
	concreteToolSchema(schema.Items)
	concreteToolSchema(schema.AdditionalProperties)
}

func commandToolDefinitions() []*mcp.Tool {
	// Derive the vocabulary from the existing command type, not a second schema.
	instructions, err := jsonschema.For[domain.CommandInstructionSet](nil)
	if err != nil {
		panic(err) // A programming error in the fixed command type.
	}
	delete(instructions.Properties, "identity")
	concreteToolSchema(instructions)
	procedures := instructions.Properties["procedures"]
	minimumProcedures := 1
	procedures.MinItems = &minimumProcedures
	// The runtime accepts omitted ignore_failure as false.
	procedure := procedures.Items
	procedure.Required = nil
	procedure.Properties["type"].Enum = []any{string(domain.ProcedureTypeContainer), string(domain.ProcedureTypeSignal)}
	run := instructions.Properties["run"]
	run.Enum = []any{string(domain.RunModeAlways), string(domain.RunModeOnce), string(domain.RunModeRestart), string(domain.RunModePersistent)}
	stringField := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	integerField := func(max int) map[string]any { return map[string]any{"type": "integer", "minimum": 1, "maximum": max} }
	tool := func(name, description string, properties map[string]any, required ...string) *mcp.Tool {
		if properties == nil {
			properties = map[string]any{}
		}
		schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) > 0 {
			schema["required"] = required
		}
		return &mcp.Tool{Name: name, Description: description, InputSchema: schema}
	}
	return []*mcp.Tool{
		tool("create_command", `Register a new persistent deployment command without executing it. Instructions define container images, arguments, environments and deployment-data mounts; this creates container workloads, not host-shell execution. Existing names and workload identity grants are rejected. Use run_command afterward. Example: {"command":"example-echo","instructions":{"procedures":[{"image":"busybox:1.37","command":["echo","hello"]}]}}`, map[string]any{"command": stringField("New command name"), "instructions": instructions}, "command", "instructions"),
		tool("list_commands", "List existing Scroll commands and console IDs, without secrets.", nil),
		tool("remove_command", "Stop a named command and remove its stored definition, queue/status entry and runtime containers. This does not delete the deployment or its data. Removing a command also removes its console/log access.", map[string]any{"command": stringField("Command name to stop and remove")}, "command"),
		tool("get_queue", "Read current command/procedure status.", nil),
		tool("get_deployment_status", "Read deployment state, installed release and ports/endpoints.", nil),
		tool("run_command", "Enqueue an existing named command; inspect its current status and logs for completion.", map[string]any{"command": stringField("Existing command name")}, "command"),
		tool("wait_for_command", "Wait up to 30 seconds for the named command's current status. A restart or replacement under the same name is not distinguished.", map[string]any{"command": stringField("Command name"), "timeout_seconds": integerField(30)}, "command"),
		tool("read_logs", "Read a finite recent console snapshot (default 200 lines, maximum 64 KiB). Application logs may contain secrets.", map[string]any{"console": stringField("Console ID from list_commands"), "tail_lines": integerField(1000)}, "console"),
		tool("search_logs", "Case-insensitive literal search of recent logs; maximum 50 matches and 1 MiB scanned.", map[string]any{"console": stringField("Console ID"), "query": stringField("Literal search text"), "tail_lines": integerField(10000)}, "console", "query"),
		tool("send_stdin", "Write exact text to an existing running console. Include a newline to submit a line. Does not start a shell.", map[string]any{"console": stringField("Console ID"), "text": stringField("Exact UTF-8 input, at most 8 KiB")}, "console", "text"),
		tool("publish_ui", "Publish an already built Druid UI WASM/OpenUI pair for this deployment. First show focused previews, explain changes and scope, ask for publishing approval, end your turn and wait for a subsequent explicit customer reply. The original UI-change request is not approval. Revisions require renewed approval; failed previews must not be published automatically. For behavior-only changes, explain the preview limitation and still obtain approval. Do not bypass approval through HTTP, terminal commands, Developer Mode or live watchers. This is an agent instruction, not a server-enforced approval check. Does not build or publish npm/Scroll artifacts. Choose private or public explicitly; path defaults to <scope>/dist/app.wasm with an adjacent .openui document. Returns the published package only after upload succeeds.", map[string]any{
			"scope": map[string]any{"type": "string", "enum": []string{"private", "public"}, "description": "UI scope; use private unless the customer requests public UI"},
			"path":  stringField("Optional scope-relative .wasm path; the matching .openui file must be adjacent"),
		}, "scope"),
	}
}
