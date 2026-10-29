package services

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"net/url"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	"go.uber.org/zap"
)

func (s *RuntimeSession) runCommand(cmd string, extraEnv ...map[string]map[string]string) error {
	command, err := s.scrollService.GetCommand(cmd)
	if err != nil {
		return err
	}

	logger.Log().Info("Running command",
		zap.String("cmd", cmd),
		zap.String("runMode", string(command.Run)),
	)

	s.mu.Lock()
	root := s.runtimeScroll.Root
	scrollID := s.runtimeScroll.ID
	scrollName := s.runtimeScroll.ScrollName
	routing := make([]domain.RuntimeRouteAssignment, len(s.runtimeScroll.Routing))
	copy(routing, s.runtimeScroll.Routing)
	reservations := append([]domain.Port(nil), s.runtimeScroll.ReservedPorts...)
	s.mu.Unlock()

	if root == "" {
		root = s.scrollService.GetCwd()
	}
	file := s.scrollService.GetFile()
	mergedPorts, err := mergeRuntimePorts(file.Ports, reservations)
	if err != nil {
		s.setCommandProcedureStatus(cmd, command, domain.ScrollLockStatusError, nil)
		return err
	}
	runtimePorts, err := resolveRuntimePorts(mergedPorts, routing, true)
	if err != nil {
		s.setCommandProcedureStatus(cmd, command, domain.ScrollLockStatusError, nil)
		return err
	}
	runtimeFile := *file
	runtimeFile.Ports = runtimePorts
	procedureEnv, err := coreservices.BuildRuntimeProcedureEnv(&runtimeFile, cmd, command, coreservices.RuntimeEnvContext{
		ScrollID:   scrollID,
		ScrollName: scrollName,
		Backend:    s.runtimeBackend.Name(),
		Routing:    routing,
	})
	if err != nil {
		s.setCommandProcedureStatus(cmd, command, domain.ScrollLockStatusError, nil)
		return err
	}
	var extraProcedureEnv map[string]map[string]string
	if len(extraEnv) > 0 {
		extraProcedureEnv = extraEnv[0]
	}
	for procedure, values := range extraProcedureEnv {
		if procedureEnv[procedure] == nil {
			procedureEnv[procedure] = map[string]string{}
		}
		for key, value := range values {
			procedureEnv[procedure][key] = value
		}
	}
	executionID := ""
	s.queueMu.Lock()
	recovering := s.queue[cmd] != nil && s.queue[cmd].recovered
	s.queueMu.Unlock()
	for _, status := range s.Snapshot()[cmd] {
		if status.ExecutionID != "" {
			executionID = status.ExecutionID
			break
		}
	}
	if executionID == "" {
		if recovering && command.Identity != nil {
			return fmt.Errorf("identity-enabled command requires a fresh execution")
		}
		executionID = uuid.NewString()
		if err := s.assignExecutionIdentity(cmd, executionID); err != nil {
			return err
		}
	}
	identity := &domain.RuntimeCommandIdentity{ExecutionID: executionID, DeploymentID: scrollID}
	s.provisioning.Store(executionID, true)
	defer s.provisioning.Delete(executionID)
	if command.Identity != nil {
		backend, ok := s.runtimeBackend.(ports.CommandIdentityBackend)
		if !ok || s.workloadAPIURL == "" {
			return fmt.Errorf("command identity requires runtime support and workload API URL")
		}
		mount, err := backend.PrepareCommandIdentity(context.Background(), workloadidentity.Record{RuntimeRoot: root, Recover: recovering, ExecutionID: executionID, DeploymentID: scrollID, Command: cmd, Permissions: command.Identity.Permissions})
		if err != nil {
			return err
		}
		identity.Enabled, identity.TokenPath = true, mount.Path
	}
	// Never mutate the persisted Scroll or leak transient mount references.
	copyCommand := *command
	copyCommand.Procedures = make([]*domain.Procedure, len(command.Procedures))
	for index, procedure := range command.Procedures {
		copyProcedure := *procedure
		copyProcedure.RuntimeIdentity = identity
		copyCommand.Procedures[index] = &copyProcedure
		name := domain.ProcedureName(cmd, index, procedure)
		if procedureEnv[name] == nil {
			procedureEnv[name] = map[string]string{}
		}
		procedureEnv[name]["DRUID_EXECUTION_ID"] = executionID
		if command.Identity != nil {
			procedureEnv[name]["DRUID_TOKEN_FILE"] = workloadidentity.TokenFile
			procedureEnv[name]["DRUID_API_URL"] = s.workloadAPIURL + "/" + url.PathEscape(scrollID)
		}
	}
	if command.Identity != nil && command.Run != domain.RunModePersistent {
		defer s.revokeExecution(executionID)
	}

	exitCode, err := s.runtimeBackend.RunCommand(ports.RuntimeCommand{
		ExecutionID:  executionID,
		Name:         cmd,
		ScrollID:     scrollID,
		Command:      &copyCommand,
		Root:         root,
		Ports:        runtimePorts,
		Routing:      routing,
		ProcedureEnv: procedureEnv,
	})
	if err != nil {
		if command.Identity != nil {
			_ = s.revokeExecution(executionID)
		}
		s.setCommandProcedureStatus(cmd, command, domain.ScrollLockStatusError, exitCode)
		return err
	}
	if exitCode != nil && *exitCode != 0 {
		s.setCommandProcedureStatus(cmd, command, domain.ScrollLockStatusError, exitCode)
		return &domain.CommandExecutionError{
			Command:  cmd,
			ExitCode: *exitCode,
			Err:      errors.New("command failed"),
		}
	}
	return nil
}

func (s *RuntimeSession) setCommandProcedureStatus(commandName string, command *domain.CommandInstructionSet, status domain.ScrollLockStatus, exitCode *int) {
	for idx, procedure := range command.Procedures {
		s.persistProcedureStatus(commandName, domain.ProcedureName(commandName, idx, procedure), status, exitCode)
	}
}
