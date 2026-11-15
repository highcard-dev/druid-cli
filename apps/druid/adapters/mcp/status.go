package commandmcp

func (m *commandMCP) addStatus() {
	m.add(commandMCPTool("get_deployment_status", "Read deployment state, installed release and ports/endpoints.", nil))
}
