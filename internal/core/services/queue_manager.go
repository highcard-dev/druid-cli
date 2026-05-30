package services

import (
	"errors"
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

var ErrAlreadyInQueue = fmt.Errorf("command is already in queue")
var ErrCommandNotFound = fmt.Errorf("command not found")
var ErrCommandDoneOnce = fmt.Errorf("command is already done and has run mode once")

type AddItemOptions struct {
	Wait  bool
	Force bool
}

type QueueManager struct {
	scrollService     ports.ScrollServiceInterface
	procedureLauncher ports.ProcedureLauchnerInterface
}

func NewQueueManager(
	scrollService ports.ScrollServiceInterface,
	procedureLauncher ports.ProcedureLauchnerInterface,
) *QueueManager {
	return &QueueManager{
		scrollService:     scrollService,
		procedureLauncher: procedureLauncher,
	}
}

func (q *QueueManager) RunCommand(cmd string) error {
	logger.Log().Debug("Running command",
		zap.String("cmd", cmd),
	)
	return q.procedureLauncher.Run(cmd)
}

func DeriveCommandStatusFromProcedures(commandName string, command *domain.CommandInstructionSet, statuses map[string]domain.LockStatus) (domain.ScrollLockStatus, bool) {
	if command == nil || len(command.Procedures) == 0 {
		return "", false
	}
	seen := false
	allDone := true
	hasRunning := false
	hasWaiting := false
	for idx, procedure := range command.Procedures {
		procedureName := domain.ProcedureName(commandName, idx, procedure)
		status, ok := statuses[procedureName]
		if !ok {
			allDone = false
			continue
		}
		seen = true
		switch status.Status {
		case domain.ScrollLockStatusError:
			return domain.ScrollLockStatusError, true
		case domain.ScrollLockStatusRunning:
			hasRunning = true
			allDone = false
		case domain.ScrollLockStatusWaiting:
			hasWaiting = true
			allDone = false
		case domain.ScrollLockStatusDone:
		default:
			allDone = false
		}
	}
	if hasRunning {
		return domain.ScrollLockStatusRunning, true
	}
	if hasWaiting || (seen && !allDone) {
		return domain.ScrollLockStatusWaiting, true
	}
	if seen && allDone {
		return domain.ScrollLockStatusDone, true
	}
	return "", false
}

func CommandExitCode(err error) *int {
	var commandErr *domain.CommandExecutionError
	if err != nil && errors.As(err, &commandErr) {
		return &commandErr.ExitCode
	}
	return nil
}
