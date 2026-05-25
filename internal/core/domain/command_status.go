package domain

import "errors"

type ScrollLockStatus string

const (
	ScrollLockStatusRunning ScrollLockStatus = "running"
	ScrollLockStatusDone    ScrollLockStatus = "done"
	ScrollLockStatusError   ScrollLockStatus = "error"
	ScrollLockStatusWaiting ScrollLockStatus = "waiting"
)

type LockStatus struct {
	Status           ScrollLockStatus `json:"status"`
	ExitCode         *int             `json:"exit_code"`
	LastStatusChange int64            `json:"last_status_change"`
}

type CommandExecutionError struct {
	Command  string
	ExitCode int
	Err      error
}

type NonRetryableCommandError struct {
	Err error
}

func (e *NonRetryableCommandError) Error() string {
	if e.Err == nil {
		return "non-retryable command error"
	}
	return e.Err.Error()
}

func (e *NonRetryableCommandError) Unwrap() error {
	return e.Err
}

func NonRetryableCommand(err error) error {
	if err == nil {
		return nil
	}
	return &NonRetryableCommandError{Err: err}
}

func IsNonRetryableCommandError(err error) bool {
	var nonRetryable *NonRetryableCommandError
	return errors.As(err, &nonRetryable)
}

func (e *CommandExecutionError) Error() string {
	if e.Err == nil {
		return "command failed"
	}
	return e.Err.Error()
}

func (e *CommandExecutionError) Unwrap() error {
	return e.Err
}
