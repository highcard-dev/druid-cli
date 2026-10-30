package commandmcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func publishUITool() *mcp.Tool {
	return commandMCPTool("publish_ui", "Publish an already built Druid UI WASM/OpenUI pair for this deployment. First show focused previews, explain changes and scope, ask for publishing approval, end your turn and wait for a subsequent explicit customer reply. The original UI-change request is not approval. Revisions require renewed approval; failed previews must not be published automatically. For behavior-only changes, explain the preview limitation and still obtain approval. Do not bypass approval through HTTP, terminal commands, Developer Mode or live watchers. This is an agent instruction, not a server-enforced approval check. Does not build or publish npm/Scroll artifacts. Choose private or public explicitly; path defaults to <scope>/dist/app.wasm with an adjacent .openui document. Returns the published package only after upload succeeds.", map[string]any{
		"scope": map[string]any{"type": "string", "enum": []string{"private", "public"}, "description": "UI scope; use private unless the customer requests public UI"},
		"path":  stringField("Optional scope-relative .wasm path; the matching .openui file must be adjacent"),
	}, "scope")
}

func (m *commandMCP) addUI() {
	m.add(publishUITool())
}
