package domain

type QueueItem struct {
	Status            ScrollLockStatus
	UpdateLockStatus  bool
	RunAfterExecution func()
}
