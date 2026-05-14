package services

import "github.com/highcard-dev/daemon/internal/core/domain"

func deriveRuntimeScrollStatus(statuses map[string]domain.LockStatus, commands map[string]*domain.CommandInstructionSet) domain.RuntimeScrollStatus {
	if len(statuses) == 0 {
		return domain.RuntimeScrollStatusCreated
	}
	hasActive := false
	hasPersistentDone := false
	for commandName, status := range statuses {
		if status.Status == domain.ScrollLockStatusError {
			return domain.RuntimeScrollStatusError
		}
		if status.Status == domain.ScrollLockStatusRunning || status.Status == domain.ScrollLockStatusWaiting {
			hasActive = true
		}
		if status.Status == domain.ScrollLockStatusDone {
			if command := commands[commandName]; command != nil && command.Run == domain.RunModePersistent {
				hasPersistentDone = true
			}
		}
	}
	if hasActive || hasPersistentDone {
		return domain.RuntimeScrollStatusRunning
	}
	return domain.RuntimeScrollStatusStopped
}

func copyCommandStatuses(statuses map[string]domain.LockStatus) map[string]domain.LockStatus {
	copied := map[string]domain.LockStatus{}
	for command, status := range statuses {
		copied[command] = status
	}
	return copied
}
