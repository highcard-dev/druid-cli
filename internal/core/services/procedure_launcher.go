package services

import (
	"errors"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ProcedureLauncher struct {
	runtimeBackend    ports.RuntimeBackendInterface
	runtimeRoot       string
	runtimeScrollID   string
	runtimeScrollName string
	routingProvider   func() []domain.RuntimeRouteAssignment
	scrollService     ports.ScrollServiceInterface
	statusObserver    ProcedureStatusObserver
}

type ProcedureStatusObserver func(command string, procedure string, status domain.ScrollLockStatus, exitCode *int)

func NewProcedureLauncher(
	scrollService ports.ScrollServiceInterface,
	runtimeBackend ports.RuntimeBackendInterface,
	runtimeRoot string,
) (*ProcedureLauncher, error) {
	return NewProcedureLauncherForScroll(scrollService, runtimeBackend, runtimeRoot, "")
}

func NewProcedureLauncherForScroll(
	scrollService ports.ScrollServiceInterface,
	runtimeBackend ports.RuntimeBackendInterface,
	runtimeRoot string,
	runtimeScrollID string,
) (*ProcedureLauncher, error) {
	return NewProcedureLauncherForRuntime(scrollService, runtimeBackend, runtimeRoot, runtimeScrollID, "", nil)
}

func NewProcedureLauncherForRuntime(
	scrollService ports.ScrollServiceInterface,
	runtimeBackend ports.RuntimeBackendInterface,
	runtimeRoot string,
	runtimeScrollID string,
	runtimeScrollName string,
	routingProvider func() []domain.RuntimeRouteAssignment,
) (*ProcedureLauncher, error) {
	if runtimeBackend == nil {
		return nil, errors.New("runtime backend is required")
	}

	s := &ProcedureLauncher{
		runtimeBackend:    runtimeBackend,
		runtimeRoot:       runtimeRoot,
		runtimeScrollID:   runtimeScrollID,
		runtimeScrollName: runtimeScrollName,
		routingProvider:   routingProvider,
		scrollService:     scrollService,
	}

	return s, nil
}

func (sc *ProcedureLauncher) SetProcedureStatusObserver(observer ProcedureStatusObserver) {
	sc.statusObserver = observer
}

func (sc *ProcedureLauncher) setProcedureStatus(command string, procedure string, status domain.ScrollLockStatus, exitCode *int) {
	observer := sc.statusObserver
	if observer != nil {
		observer(command, procedure, status, exitCode)
	}
}

func (sc *ProcedureLauncher) Run(cmd string) error {
	command, err := sc.scrollService.GetCommand(cmd)
	if err != nil {
		return err
	}

	logger.Log().Info("Running command",
		zap.String("cmd", cmd),
		zap.String("runMode", string(command.Run)),
	)

	root := sc.runtimeRoot
	if root == "" {
		root = sc.scrollService.GetCwd()
	}
	file := sc.scrollService.GetFile()
	routing := []domain.RuntimeRouteAssignment{}
	if sc.routingProvider != nil {
		routing = sc.routingProvider()
	}
	procedureEnv, err := BuildRuntimeProcedureEnv(file, cmd, command, RuntimeEnvContext{
		ScrollID:   sc.runtimeScrollID,
		ScrollName: sc.runtimeScrollName,
		Backend:    sc.runtimeBackend.Name(),
		Routing:    routing,
	})
	if err != nil {
		sc.setCommandProcedureStatus(cmd, command, domain.ScrollLockStatusError, nil)
		return err
	}
	exitCode, err := sc.runtimeBackend.RunCommand(ports.RuntimeCommand{
		Name:         cmd,
		ScrollID:     sc.runtimeScrollID,
		Command:      command,
		Root:         root,
		GlobalPorts:  file.Ports,
		Routing:      routing,
		ProcedureEnv: procedureEnv,
		ProcedureStatusObserver: func(procedure string, status domain.ScrollLockStatus, exitCode *int) {
			sc.setProcedureStatus(cmd, procedure, status, exitCode)
		},
	})
	if err != nil {
		sc.setCommandProcedureStatus(cmd, command, domain.ScrollLockStatusError, exitCode)
		return err
	}
	if exitCode != nil && *exitCode != 0 {
		sc.setCommandProcedureStatus(cmd, command, domain.ScrollLockStatusError, exitCode)
		return &domain.CommandExecutionError{
			Command:  cmd,
			ExitCode: *exitCode,
			Err:      errors.New("command failed"),
		}
	}
	return nil
}

func (sc *ProcedureLauncher) setCommandProcedureStatus(commandName string, command *domain.CommandInstructionSet, status domain.ScrollLockStatus, exitCode *int) {
	for idx, procedure := range command.Procedures {
		sc.setProcedureStatus(commandName, domain.ProcedureName(commandName, idx, procedure), status, exitCode)
	}
}
