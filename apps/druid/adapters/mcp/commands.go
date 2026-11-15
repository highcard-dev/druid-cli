package commandmcp

import (
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func createCommandTool() *mcp.Tool {
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
	return commandMCPTool("create_command", `Register a new persistent deployment command without executing it. Instructions define container images, arguments, environments and deployment-data mounts; this creates container workloads, not host-shell execution. Existing names and workload identity grants are rejected. Use run_command afterward. Example: {"command":"example-echo","instructions":{"procedures":[{"image":"busybox:1.37","command":["echo","hello"]}]}}`, map[string]any{"command": stringField("New command name"), "instructions": instructions}, "command", "instructions")
}

func waitForCommandTool() *mcp.Tool {
	return commandMCPTool("wait_for_command", "Wait up to 30 seconds for the named command's current status. A restart or replacement under the same name is not distinguished.", map[string]any{"command": stringField("Command name"), "timeout_seconds": integerField(30)}, "command")
}

func (m *commandMCP) addCommands() {
	m.add(createCommandTool())
	m.add(commandMCPTool("list_commands", "List existing Scroll commands and console IDs, without secrets.", nil))
	m.add(commandMCPTool("remove_command", "Stop a named command and remove its stored definition, queue/status entry and runtime containers. This does not delete the deployment or its data. Removing a command also removes its console/log access.", map[string]any{"command": stringField("Command name to stop and remove")}, "command"))
	m.add(commandMCPTool("get_queue", "Read current command/procedure status.", nil))
	m.add(commandMCPTool("run_command", "Enqueue an existing named command; inspect its current status and logs for completion.", map[string]any{"command": stringField("Existing command name")}, "command"))
	m.add(waitForCommandTool())
}
