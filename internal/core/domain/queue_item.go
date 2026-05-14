package domain

type QueueItem struct {
	Name         string
	Status       ScrollLockStatus
	Error        error
	DoneChan     chan struct{}
	RestartCount uint
}
