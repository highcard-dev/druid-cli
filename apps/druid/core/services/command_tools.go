package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
)

type ToolInput struct {
	Scope          string                        `json:"scope,omitempty"`
	Path           string                        `json:"path,omitempty"`
	Instructions   *domain.CommandInstructionSet `json:"instructions,omitempty"`
	Command        string                        `json:"command,omitempty"`
	ExecutionID    string                        `json:"execution_id,omitempty"`
	Console        string                        `json:"console,omitempty"`
	Text           string                        `json:"text,omitempty"`
	Query          string                        `json:"query,omitempty"`
	TailLines      int                           `json:"tail_lines,omitempty"`
	TimeoutSeconds int                           `json:"timeout_seconds,omitempty"`
}

var ToolPermissions = map[string]string{
	"publish_ui":     "ui:publish",
	"create_command": "commands:create",
	"list_commands":  "commands:list", "get_queue": "queue:read", "run_command": "commands:run", "wait_for_command": "queue:read",
	"read_logs": "logs:read", "search_logs": "logs:read", "send_stdin": "console:write", "get_deployment_status": "runtime:read",
}

type ToolError struct {
	Status  int
	Message string
}

func (e *ToolError) Error() string               { return e.Message }
func toolError(status int, message string) error { return &ToolError{Status: status, Message: message} }

// CallCommandTool is the single permission/execution seam shared by HTTP and MCP.
func (s *RuntimeSupervisor) CallCommandTool(ctx context.Context, token, deployment, name string, input ToolInput) (any, error) {
	record, err := s.AuthenticateCommand(ctx, token, deployment)
	if err != nil {
		return nil, err
	}
	permission, known := ToolPermissions[name]
	if !known {
		return nil, toolError(404, "unknown tool")
	}
	if !record.Allows(permission) {
		return nil, toolError(403, "workload permission denied")
	}
	switch name {
	case "publish_ui":
		scope, path, err := normalizeUIPackageRequest(input.Scope, input.Path)
		if err != nil {
			return nil, toolError(400, err.Error())
		}
		updated, err := s.PublishUIPackage(deployment, string(scope), path)
		if err != nil {
			return nil, err
		}
		return map[string]any{"scope": scope, "published": true, "package": updated.UIPackages[scope]}, nil
	case "create_command":
		return s.createToolCommand(deployment, input)
	case "list_commands":
		file, err := s.ScrollFile(deployment)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(file.Commands))
		for name := range file.Commands {
			names = append(names, name)
		}
		sort.Strings(names)
		commands := make([]map[string]any, 0, len(names))
		for _, name := range names {
			command := file.Commands[name]
			consoles := []string{}
			for index, procedure := range command.Procedures {
				if procedure.IsContainer() {
					consoles = append(consoles, domain.ProcedureName(name, index, procedure))
				}
			}
			commands = append(commands, map[string]any{"name": name, "run": command.Run, "needs": command.Needs, "consoles": consoles})
		}
		return map[string]any{"commands": commands}, nil
	case "get_queue":
		return s.Queue(deployment)
	case "run_command":
		return s.enqueueToolCommand(deployment, input.Command)
	case "wait_for_command":
		return s.waitToolCommand(ctx, token, deployment, input)
	case "get_deployment_status":
		runtime, err := s.Get(deployment)
		if err != nil {
			return nil, err
		}
		ports, err := s.Ports(deployment)
		if err != nil {
			return nil, err
		}
		// Deliberately project fields: RuntimeScroll includes private host paths.
		publicPorts := make([]map[string]any, 0, len(ports))
		for _, port := range ports {
			publicPorts = append(publicPorts, map[string]any{"name": port.Name, "procedure": port.Procedure, "port": port.Port, "protocol": port.Protocol, "bound": port.Bound})
		}
		errorSummary := ""
		if runtime.LastError != "" {
			errorSummary = "Deployment runtime reported an error"
		}
		return map[string]any{"deployment_id": runtime.ID, "status": runtime.Status, "release": map[string]string{"artifact": runtime.Artifact, "digest": runtime.ArtifactDigest}, "ports": publicPorts, "endpoints": runtime.Routing, "error": errorSummary}, nil
	case "read_logs", "search_logs", "send_stdin":
		return s.consoleTool(ctx, deployment, name, input)
	}
	return nil, toolError(404, "unknown tool")
}

func (s *RuntimeSupervisor) createToolCommand(deployment string, input ToolInput) (any, error) {
	if input.Command == "" || input.Instructions == nil {
		return nil, toolError(400, "command and instructions are required")
	}
	if input.Instructions.Identity != nil {
		return nil, toolError(400, "created commands cannot request workload identity")
	}
	unlock := s.lockRuntimeOperation(deployment)
	defer unlock()
	session, err := s.sessionFor(deployment)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	_, exists := session.scrollService.GetFile().Commands[input.Command]
	session.mu.Unlock()
	if exists {
		return nil, toolError(409, "command already exists")
	}
	if err := session.AddCommand(input.Command, input.Instructions); err != nil {
		return nil, toolError(400, err.Error())
	}
	return map[string]any{"command": input.Command, "created": true}, nil
}

func (s *RuntimeSupervisor) enqueueToolCommand(deployment, command string) (any, error) {
	if command == "" {
		return nil, toolError(400, "command is required")
	}
	unlock := s.lockRuntimeOperation(deployment)
	defer unlock()
	session, err := s.sessionFor(deployment)
	if err != nil {
		return nil, err
	}
	definition, err := session.scrollService.GetCommand(command)
	if err != nil {
		return nil, toolError(404, "command is unavailable")
	}
	session.Start()
	session.rememberDoneDependencies(definition, map[string]bool{})
	execution := uuid.NewString()
	_, err = session.enqueueExecution(command, coreservices.AddItemOptions{}, nil, execution)
	if errors.Is(err, coreservices.ErrAlreadyInQueue) || errors.Is(err, coreservices.ErrCommandDoneOnce) {
		return nil, toolError(409, err.Error())
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"command": command, "execution_id": execution, "accepted": true}, nil
}

func (s *RuntimeSupervisor) waitToolCommand(ctx context.Context, token, deployment string, input ToolInput) (any, error) {
	if input.Command == "" || input.ExecutionID == "" {
		return nil, toolError(400, "command and execution_id are required")
	}
	timeout := input.TimeoutSeconds
	if timeout == 0 {
		timeout = 10
	}
	if timeout < 1 || timeout > 30 {
		return nil, toolError(400, "timeout_seconds must be between 1 and 30")
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for {
		record, err := s.AuthenticateCommand(ctx, token, deployment)
		if err != nil {
			return nil, err
		}
		if !record.Allows("queue:read") {
			return nil, toolError(403, "workload permission denied")
		}
		queue, err := s.Queue(deployment)
		if err != nil {
			return nil, err
		}
		statuses := queue[input.Command]
		if len(statuses) == 0 {
			return nil, toolError(410, "execution stopped or unavailable")
		}
		state := domain.ScrollLockStatusDone
		var exitCode *int
		for _, status := range statuses {
			if status.ExecutionID != input.ExecutionID {
				return nil, toolError(409, "execution was replaced")
			}
			if status.Status == domain.ScrollLockStatusError {
				state = domain.ScrollLockStatusError
				exitCode = status.ExitCode
				break
			}
			if status.Status != domain.ScrollLockStatusDone {
				state = status.Status
			}
		}
		if state == domain.ScrollLockStatusDone {
			zero := 0
			exitCode = &zero
		}
		terminal := state == domain.ScrollLockStatusDone || state == domain.ScrollLockStatusError
		timedOut := !terminal && !time.Now().Before(deadline)
		if terminal || timedOut {
			return map[string]any{"command": input.Command, "execution_id": input.ExecutionID, "status": state, "exit_code": exitCode, "timed_out": timedOut}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *RuntimeSupervisor) consoleTool(ctx context.Context, deployment, name string, input ToolInput) (any, error) {
	if input.Console == "" {
		return nil, toolError(400, "console is required")
	}
	backend, ok := s.runtimeBackend.(ports.CommandConsoleBackend)
	if !ok {
		return nil, toolError(503, "runtime console tools are unavailable")
	}
	consoles, err := s.Consoles(deployment)
	if err != nil {
		return nil, err
	}
	if _, ok := consoles[input.Console]; !ok {
		return nil, toolError(404, "console is unavailable")
	}
	runtime, err := s.Get(deployment)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if name == "send_stdin" {
		if len(input.Text) > 8192 || !utf8.ValidString(input.Text) {
			return nil, toolError(400, "stdin must be valid UTF-8 and at most 8 KiB")
		}
		count, err := backend.WriteConsoleInput(ctx, runtime.Root, input.Console, []byte(input.Text))
		if err != nil {
			return nil, toolError(409, "console input is unavailable")
		}
		return map[string]any{"console": input.Console, "bytes_accepted": count}, nil
	}
	tail, maxTail, maxBytes := input.TailLines, 1000, int64(64*1024)
	if name == "search_logs" {
		maxTail = 10000
		maxBytes = 1024 * 1024
		if tail == 0 {
			tail = 1000
		}
		if input.Query == "" || len(input.Query) > 4096 {
			return nil, toolError(400, "query is required and must be at most 4 KiB")
		}
	}
	if tail == 0 {
		tail = 200
	}
	if tail < 1 || tail > maxTail {
		return nil, toolError(400, fmt.Sprintf("tail_lines must be between 1 and %d", maxTail))
	}
	output, truncated, err := backend.ReadConsoleLogs(ctx, runtime.Root, input.Console, tail, maxBytes)
	if err != nil {
		return nil, toolError(409, "console output is unavailable")
	}
	output = workloadidentity.Redact(output)
	if name == "read_logs" {
		return map[string]any{"console": input.Console, "output": output, "truncated": truncated}, nil
	}
	matches := []map[string]any{}
	bytes := 0
	query := strings.ToLower(input.Query)
	lines := strings.Split(output, "\n")
	for index, line := range lines {
		if !strings.Contains(strings.ToLower(line), query) {
			continue
		}
		if len(matches) >= 50 || bytes+len(line) > 64*1024 {
			truncated = true
			break
		}
		matches = append(matches, map[string]any{"line": index + 1, "text": line})
		bytes += len(line)
	}
	return map[string]any{"console": input.Console, "matches": matches, "scanned_lines": len(lines), "truncated": truncated}, nil
}
