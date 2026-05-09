package domain

type QueueItem struct {
	Name              string
	Status            ScrollLockStatus
	Error             error
	RunAfterExecution func()
	DoneChan          chan struct{}
	RestartCount      uint
}
