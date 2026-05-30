package services

import "github.com/highcard-dev/daemon/internal/core/domain"

func deriveRuntimeScrollStatus(procedures domain.ProcedureStatusMap, commands map[string]*domain.CommandInstructionSet) domain.RuntimeScrollStatus {
	if len(procedures) == 0 {
		return domain.RuntimeScrollStatusCreated
	}
	queue := deriveCommandStatuses(procedures, commands)
	if len(queue) == 0 {
		return domain.RuntimeScrollStatusCreated
	}
	hasActive := false
	for _, status := range queue {
		switch status {
		case domain.ScrollLockStatusError:
			return domain.RuntimeScrollStatusError
		case domain.ScrollLockStatusRunning, domain.ScrollLockStatusWaiting:
			hasActive = true
		}
	}
	if hasActive {
		return domain.RuntimeScrollStatusRunning
	}
	return domain.RuntimeScrollStatusStopped
}

func deriveCommandStatuses(procedures domain.ProcedureStatusMap, commands map[string]*domain.CommandInstructionSet) map[string]domain.ScrollLockStatus {
	queue := map[string]domain.ScrollLockStatus{}
	for commandName, command := range commands {
		status, ok := deriveCommandStatus(procedures[commandName], commandName, command)
		if ok {
			queue[commandName] = status
		}
	}
	return queue
}

func deriveCommandStatus(statuses map[string]domain.LockStatus, commandName string, command *domain.CommandInstructionSet) (domain.ScrollLockStatus, bool) {
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

func copyProcedureStatuses(statuses domain.ProcedureStatusMap) domain.ProcedureStatusMap {
	copied := domain.ProcedureStatusMap{}
	for command, procedures := range statuses {
		copied[command] = map[string]domain.LockStatus{}
		for procedure, status := range procedures {
			copied[command][procedure] = status
		}
	}
	return copied
}
