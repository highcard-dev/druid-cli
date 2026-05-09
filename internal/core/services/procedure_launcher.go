package services

import (
	"errors"
	"sync"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ProcedureLauncher struct {
	runtimeBackend  ports.RuntimeBackendInterface
	runtimeDataRoot string
	scrollService   ports.ScrollServiceInterface
	procedures      map[string]domain.ScrollLockStatus
	proceduresMutex *sync.Mutex
}

func NewProcedureLauncher(
	scrollService ports.ScrollServiceInterface,
	runtimeBackend ports.RuntimeBackendInterface,
	runtimeDataRoot string,
) (*ProcedureLauncher, error) {
	if runtimeBackend == nil {
		return nil, errors.New("runtime backend is required")
	}

	s := &ProcedureLauncher{
		runtimeBackend:  runtimeBackend,
		runtimeDataRoot: runtimeDataRoot,
		scrollService:   scrollService,
		procedures:      make(map[string]domain.ScrollLockStatus),
		proceduresMutex: &sync.Mutex{},
	}

	return s, nil
}

func (sc *ProcedureLauncher) setProcedureStatus(procedure string, status domain.ScrollLockStatus) {
	sc.proceduresMutex.Lock()
	defer sc.proceduresMutex.Unlock()
	sc.procedures[procedure] = status
}

func (sc *ProcedureLauncher) GetProcedureStatuses() map[string]domain.ScrollLockStatus {
	sc.proceduresMutex.Lock()
	defer sc.proceduresMutex.Unlock()
	return sc.procedures
}

func (sc *ProcedureLauncher) Run(cmd string) error {
	command, err := sc.scrollService.GetCommand(cmd)
	if err != nil {
		sc.setProcedureStatus(cmd, domain.ScrollLockStatusError)
		return err
	}

	logger.Log().Info("Running command",
		zap.String("cmd", cmd),
		zap.String("runMode", string(command.Run)),
	)

	dataRoot := sc.runtimeDataRoot
	if dataRoot == "" {
		dataRoot = sc.scrollService.GetCwd()
	}
	sc.setProcedureStatus(cmd, domain.ScrollLockStatusRunning)
	exitCode, err := sc.runtimeBackend.RunCommand(ports.RuntimeCommand{
		Name:        cmd,
		Command:     command,
		DataRoot:    dataRoot,
		GlobalPorts: sc.scrollService.GetFile().Ports,
	})
	if err != nil {
		sc.setProcedureStatus(cmd, domain.ScrollLockStatusError)
		return err
	}
	if exitCode != nil && *exitCode != 0 {
		sc.setProcedureStatus(cmd, domain.ScrollLockStatusError)
		return &domain.CommandExecutionError{
			Command:  cmd,
			ExitCode: *exitCode,
			Err:      errors.New("command failed"),
		}
	}
	sc.setProcedureStatus(cmd, domain.ScrollLockStatusDone)
	return nil
}
