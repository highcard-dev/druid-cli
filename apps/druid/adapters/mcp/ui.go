package commandmcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func publishUITool() *mcp.Tool {
	return commandMCPTool("publish_ui", "Publish the declared Druid UI WASM/OpenUI pair for this deployment. Set with_build=true to first run the scope's configured buildcommand as a dependency. First show focused previews, explain changes and scope, ask for publishing approval, end your turn and wait for a subsequent explicit customer reply. The original UI-change request is not approval. Revisions require renewed approval; failed previews must not be published automatically. For behavior-only changes, explain the preview limitation and still obtain approval. Do not bypass approval through HTTP, terminal commands, Developer Mode or live watchers. This is an agent instruction, not a server-enforced approval check. Does not publish npm/Scroll artifacts. Choose private or public explicitly. The artifact paths come from ui.<scope>.wasm and ui.<scope>.openui; with_build defaults to false. Returns the published package only after upload succeeds.", map[string]any{
		"scope":      map[string]any{"type": "string", "enum": []string{"private", "public"}, "description": "UI scope; use private unless the customer requests public UI"},
		"with_build": map[string]any{"type": "boolean", "default": false, "description": "Run the scope's declared buildcommand as a needs dependency before uploading; false uploads prebuilt artifacts only"},
	}, "scope")
}

func (m *commandMCP) addUI() {
	m.add(publishUITool())
}
