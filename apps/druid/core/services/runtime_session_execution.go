package services

import (
	"errors"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (s *RuntimeSession) runCommand(cmd string) error {
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
	s.mu.Unlock()

	if root == "" {
		root = s.scrollService.GetCwd()
	}
	file := s.scrollService.GetFile()
	procedureEnv, err := coreservices.BuildRuntimeProcedureEnv(file, cmd, command, coreservices.RuntimeEnvContext{
		ScrollID:   scrollID,
		ScrollName: scrollName,
		Backend:    s.runtimeBackend.Name(),
		Routing:    routing,
	})
	if err != nil {
		s.setCommandProcedureStatus(cmd, command, domain.ScrollLockStatusError, nil)
		return err
	}

	exitCode, err := s.runtimeBackend.RunCommand(ports.RuntimeCommand{
		Name:         cmd,
		ScrollID:     scrollID,
		Command:      command,
		Root:         root,
		GlobalPorts:  file.Ports,
		Routing:      routing,
		ProcedureEnv: procedureEnv,
		ProcedureStatusObserver: func(procedure string, status domain.ScrollLockStatus, exitCode *int) {
			s.persistProcedureStatus(cmd, procedure, status, exitCode)
		},
	})
	if err != nil {
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
