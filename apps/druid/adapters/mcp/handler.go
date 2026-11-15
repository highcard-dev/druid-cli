package commandmcp

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/highcard-dev/daemon/apps/druid/adapters/toolrequest"
	services "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type commandMCP struct {
	server            *mcp.Server
	supervisor        *services.RuntimeSupervisor
	identity          workloadidentity.Identity
	deployment, token string
}

func NewHandler(supervisor *services.RuntimeSupervisor) fiber.Handler {
	return func(c *fiber.Ctx) error {
		deployment, token := c.Params("id"), toolrequest.Bearer(c.Get("Authorization"))
		record, err := supervisor.AuthenticateCommand(c.UserContext(), token, deployment)
		if err != nil {
			return fiber.NewError(toolrequest.Status(err), toolrequest.Message(err))
		}
		handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
			tools := commandMCP{
				server:     mcp.NewServer(&mcp.Implementation{Name: "druid", Version: "1.0.0"}, nil),
				supervisor: supervisor, identity: record, deployment: deployment, token: token,
			}
			tools.addCommands()
			tools.addConsole()
			tools.addStatus()
			tools.addUI()
			return tools.server
		}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
		return adaptor.HTTPHandler(handler)(c)
	}
}

// add keeps discovery permission-filtered; every call is authorized by the shared module.
func (m *commandMCP) add(definition *mcp.Tool) {
	name := definition.Name
	if !m.identity.Allows(services.ToolPermissions[name]) {
		return
	}
	m.server.AddTool(definition, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		input, err := toolrequest.Decode(request.Params.Arguments)
		if err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "invalid tool request"}}}, nil
		}
		result, err := m.supervisor.CallCommandTool(ctx, m.token, m.deployment, name, input)
		if err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: toolrequest.Message(err)}}}, nil
		}
		data, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
	})
}
