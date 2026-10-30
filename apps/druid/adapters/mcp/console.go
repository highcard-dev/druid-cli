package commandmcp

func (m *commandMCP) addConsole() {
	m.add(commandMCPTool("read_logs", "Read a finite recent console snapshot (default 200 lines, maximum 64 KiB). Application logs may contain secrets.", map[string]any{"console": stringField("Console ID from list_commands"), "tail_lines": integerField(1000)}, "console"))
	m.add(commandMCPTool("search_logs", "Case-insensitive literal search of recent logs; maximum 50 matches and 1 MiB scanned.", map[string]any{"console": stringField("Console ID"), "query": stringField("Literal search text"), "tail_lines": integerField(10000)}, "console", "query"))
	m.add(commandMCPTool("send_stdin", "Write exact text to an existing running console. Include a newline to submit a line. Does not start a shell.", map[string]any{"console": stringField("Console ID"), "text": stringField("Exact UTF-8 input, at most 8 KiB")}, "console", "text"))
}
