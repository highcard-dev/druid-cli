package domain

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

func (e *CommandExecutionError) Error() string {
	if e.Err == nil {
		return "command failed"
	}
	return e.Err.Error()
}

func (e *CommandExecutionError) Unwrap() error {
	return e.Err
}
