package domain

type QueueItem struct {
	Name              string
	Status            ScrollLockStatus
	Error             error
	UpdateLockStatus  bool
	RunAfterExecution func()
	DoneChan          chan struct{}
	RestartCount      uint
}
