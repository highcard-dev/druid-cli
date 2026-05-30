package services

import (
	"errors"
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

var ErrAlreadyInQueue = fmt.Errorf("command is already in queue")
var ErrCommandNotFound = fmt.Errorf("command not found")
var ErrCommandDoneOnce = fmt.Errorf("command is already done and has run mode once")

type AddItemOptions struct {
	Wait  bool
	Force bool
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
