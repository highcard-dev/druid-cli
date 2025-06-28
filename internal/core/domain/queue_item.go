package domain

type QueueItem struct {
	Status            ScrollLockStatus
	Error             error
	UpdateLockStatus  bool
	RunAfterExecution func()
}
