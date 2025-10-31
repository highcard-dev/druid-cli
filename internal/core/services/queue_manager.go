package services

import (
	"fmt"
	"sync"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

var ErrAlreadyInQueue = fmt.Errorf("command is already in queue")
var ErrCommandNotFound = fmt.Errorf("command not found")
var ErrCommandDoneOnce = fmt.Errorf("command is already done and has run mode once")

type AddItemOptions struct {
	Remember          bool
	Wait              bool
	RunAfterExecution func()
}

type QueueManager struct {
	mu               sync.Mutex
	runQueueMu       sync.Mutex
	scrollService    ports.ScrollServiceInterface
	processLauncher  ports.ProcedureLauchnerInterface
	commandQueue     map[string]*domain.QueueItem
	taskChan         chan string
	taskDoneChan     chan struct{}
	shutdownChan     chan struct{}
	notifierChan     []chan []string
	callbacksPostRun map[string]func()
}

func NewQueueManager(
	scrollService ports.ScrollServiceInterface,
	processLauncher ports.ProcedureLauchnerInterface,
) *QueueManager {
	return &QueueManager{
		scrollService:    scrollService,
		processLauncher:  processLauncher,
		commandQueue:     make(map[string]*domain.QueueItem),
		taskChan:         make(chan string, 100), // FIXED: Buffered channel
		taskDoneChan:     make(chan struct{}, 1), // FIXED: Buffered channel
		shutdownChan:     make(chan struct{}),
		notifierChan:     make([]chan []string, 0),
		callbacksPostRun: make(map[string]func()),
	}
}

func (sc *QueueManager) workItem(cmd string) error {
	queueItem := sc.GetQueueItem(cmd)
	if queueItem == nil {
		return fmt.Errorf("command %s not found", cmd)
	}
	changeStatus := queueItem.UpdateLockStatus

	logger.Log().Debug("Running command",
		zap.String("cmd", cmd),
		zap.Bool("changeStatus", changeStatus),
	)

	return sc.processLauncher.Run(cmd, func(cmd string) error {
		return sc.AddTempItem(cmd)
	})
}

func (sc *QueueManager) notify() {
	sc.mu.Lock()
	queuedCommands := make([]string, 0)

	for cmd, item := range sc.commandQueue {
		if item.Status != domain.ScrollLockStatusDone && item.Status != domain.ScrollLockStatusError {
			queuedCommands = append(queuedCommands, cmd)
		}
	}

	notifiers := make([]chan []string, len(sc.notifierChan))
	copy(notifiers, sc.notifierChan)
	sc.mu.Unlock()

	for _, notifier := range notifiers {
		select {
		case notifier <- queuedCommands:
			// Successfully sent queuedCommands to the notifier channel
		default:
			// The notifier channel is not ready to receive, handle accordingly
			// For example, log a warning or skip this notifier
		}
	}
}

func (sc *QueueManager) AddTempItem(cmd string) error {
	return sc.addQueueItem(cmd, AddItemOptions{
		Remember: false,
	})
}

func (sc *QueueManager) AddAndRememberItem(cmd string) error {
	return sc.addQueueItem(cmd, AddItemOptions{
		Remember: true,
	})
}

func (sc *QueueManager) AddShutdownItem(cmd string) error {
	return sc.addQueueItem(cmd, AddItemOptions{
		RunAfterExecution: func() {
			sc.Shutdown()
		},
	})
}

func (sc *QueueManager) AddItemWithCallback(cmd string, cb func()) error {
	return sc.addQueueItem(cmd, AddItemOptions{
		RunAfterExecution: cb,
	})
}

func (sc *QueueManager) AddTempItemWithWait(cmd string) error {
	return sc.addQueueItem(cmd, AddItemOptions{
		Remember: false,
		Wait:     true,
	})
}

func (sc *QueueManager) addQueueItem(cmd string, options AddItemOptions) error {
	sc.mu.Lock()

	setLock := options.Remember

	logger.Log().Debug("Running command",
		zap.String("cmd", cmd),
	)

	command, err := sc.scrollService.GetCommand(cmd)

	if err != nil {
		sc.mu.Unlock()
		return err
	}

	//Functions that run once, should be remembered, but should only have waiting status, when the are called explicitly
	if command.Run == domain.RunModeOnce {
		setLock = true
	}

	if value, ok := sc.commandQueue[cmd]; ok {

		if value.Status != domain.ScrollLockStatusDone && value.Status != domain.ScrollLockStatusError {
			sc.mu.Unlock()
			return ErrAlreadyInQueue
		}

		if value.Status == domain.ScrollLockStatusDone && command.Run == domain.RunModeOnce {
			sc.mu.Unlock()
			return ErrCommandDoneOnce
		}
	}

	var doneChan chan struct{}
	if options.Wait {
		doneChan = make(chan struct{})
	}

	item := &domain.QueueItem{
		Status:           domain.ScrollLockStatusWaiting,
		UpdateLockStatus: setLock,
		DoneChan:         doneChan,
	}

	if options.RunAfterExecution != nil {
		item.RunAfterExecution = options.RunAfterExecution
	}

	sc.commandQueue[cmd] = item

	if setLock {
		lock, err := sc.scrollService.GetLock()
		if err != nil {
			sc.mu.Unlock()
			return err
		}
		lock.SetStatus(cmd, domain.ScrollLockStatusWaiting, nil)
	}

	sc.mu.Unlock()

	// FIXED: Non-blocking send to buffered channel
	sc.taskChan <- cmd

	// Wait for completion if requested
	if options.Wait {
		<-doneChan
		// Return error if command failed
		item := sc.GetQueueItem(cmd)
		if item != nil && item.Error != nil {
			return item.Error
		}
	}

	return nil
}

func (sc *QueueManager) RegisterCallbacks(callbacks map[string]func()) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	for cmd, cb := range callbacks {
		sc.callbacksPostRun[cmd] = cb
	}
}

func (sc *QueueManager) QueueLockFile() error {
	lock, err := sc.scrollService.GetLock()

	if err != nil {
		return err
	}
	for cmd, status := range lock.Statuses {
		//convert legacy command names
		command, err := sc.scrollService.GetCommand(cmd)
		if err != nil {
			return err
		}

		if status.Status == domain.ScrollLockStatusDone {
			//check callback
			if callback, ok := sc.callbacksPostRun[cmd]; ok && callback != nil {
				callback()
			}

			//not sure if this can even happen for "restart", maybe on updates
			if command.Run != domain.RunModeRestart && command.Run != domain.RunModePersistent {

				//TODO: use addQueueItem here
				sc.mu.Lock()
				sc.commandQueue[cmd] = &domain.QueueItem{
					Status:           domain.ScrollLockStatusDone,
					UpdateLockStatus: true,
				}
				sc.mu.Unlock()
				continue
			}
		}
		status.Status = domain.ScrollLockStatusWaiting

		sc.addQueueItem(cmd, AddItemOptions{
			Remember: true,
		})
	}

	return nil
}

func (sc *QueueManager) Work() {

	for {
		select {
		case <-sc.taskChan:
			go (func() {
				sc.RunQueue()
				sc.notify()
			})()
		case <-sc.taskDoneChan:
			go (func() {
				sc.RunQueue()
				sc.notify()
			})()
		case <-sc.shutdownChan:
			//empty queue
			sc.commandQueue = make(map[string]*domain.QueueItem)
			return
		}
	}
}

func (sc *QueueManager) RunQueue() {
	sc.runQueueMu.Lock()
	defer sc.runQueueMu.Unlock()

	sc.mu.Lock()

	queueKeys := make(map[string]domain.ScrollLockStatus, len(sc.commandQueue))
	for k, v := range sc.commandQueue {
		queueKeys[k] = v.Status
	}

	sc.mu.Unlock()

	logger.Log().Info("Running queue", zap.Any("queueKeys", queueKeys))

	for cmd, status := range queueKeys {

		//if already running, skip
		if status == domain.ScrollLockStatusRunning {
			continue
		}

		command, err := sc.scrollService.GetCommand(cmd)
		if err != nil {
			logger.Log().Error("Error getting command",
				zap.String("command", cmd),
				zap.Error(err),
			)
			continue
		}

		//if run Mode is restart, we need to run it again
		if status == domain.ScrollLockStatusError {
			continue
		}

		//if run Mode is restart, we need to run it again
		if status == domain.ScrollLockStatusDone && command.Run != domain.RunModeRestart {
			continue
		}

		dependencies := command.Needs
		dependenciesReady := true
		for _, dep := range dependencies {
			_, ok := sc.commandQueue[dep]
			//if item not in queue, add it and
			if !ok {
				dependenciesReady = false
				sc.AddTempItem(dep)
				continue
			}

			if sc.getStatus(dep) != domain.ScrollLockStatusDone {
				dependenciesReady = false
				continue
			}
		}
		if dependenciesReady {
			item := sc.GetQueueItem(cmd)
			//we only run one process at a time, this is not optimal, but it is simple
			sc.setStatus(cmd, domain.ScrollLockStatusRunning, item.UpdateLockStatus)
			logger.Log().Info("Running command", zap.String("command", cmd))
			go func(c string, i *domain.QueueItem) {
				defer func() {
					// Signal completion if someone is waiting
					if i.DoneChan != nil {
						close(i.DoneChan)
					}

					if i.RunAfterExecution != nil {
						i.RunAfterExecution()
					}
					if callback, ok := sc.callbacksPostRun[c]; ok && callback != nil {
						callback()
					}

					// FIXED: Non-blocking send to buffered channel
					sc.taskDoneChan <- struct{}{}
				}()
				err := sc.workItem(c)
				if err != nil {
					sc.setError(c, err, i.UpdateLockStatus)
					logger.Log().Error("Error running command", zap.String("command", c), zap.Error(err))
					return
				}

				//restart means we are never done!
				if command.Run == domain.RunModeRestart {
					logger.Log().Info("Command done, restarting..", zap.String("command", c))
					sc.setStatus(c, domain.ScrollLockStatusWaiting, i.UpdateLockStatus)
				} else {
					logger.Log().Info("Command done", zap.String("command", c))
					sc.setStatus(c, domain.ScrollLockStatusDone, i.UpdateLockStatus)
				}

			}(cmd, item)
		} else {
			logger.Log().Info("Dependencies not ready", zap.String("command", cmd))
		}
	}
}

func (sc *QueueManager) Shutdown() {
	sc.shutdownChan <- struct{}{}
}

func (sc *QueueManager) WaitUntilEmpty() {
	notifier := make(chan []string, 10) // FIXED: Buffered channel

	sc.mu.Lock()
	sc.notifierChan = append(sc.notifierChan, notifier)
	sc.mu.Unlock()

	for {
		sc.mu.Lock()
		for cmd, item := range sc.commandQueue {
			println(cmd + ": " + string(item.Status))
		}
		sc.mu.Unlock()

		cmds := <-notifier
		if len(cmds) == 0 {
			// remove notifier
			sc.mu.Lock()
			for i, n := range sc.notifierChan {
				if n == notifier {
					sc.notifierChan = append(sc.notifierChan[:i], sc.notifierChan[i+1:]...)
					break
				}
			}
			sc.mu.Unlock()
			return
		}
	}

}

func (sc *QueueManager) GetQueueItem(cmd string) *domain.QueueItem {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if value, ok := sc.commandQueue[cmd]; ok {
		return value
	}

	return nil
}

func (sc *QueueManager) getStatus(cmd string) domain.ScrollLockStatus {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if value, ok := sc.commandQueue[cmd]; ok {
		return value.Status
	}
	return domain.ScrollLockStatusDone
}

func (sc *QueueManager) setError(cmd string, err error, writeLock bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if value, ok := sc.commandQueue[cmd]; ok {
		value.Status = domain.ScrollLockStatusError
		value.Error = err
	}
	if writeLock {
		lock, err := sc.scrollService.GetLock()
		if err != nil {
			return
		}
		lock.SetStatus(cmd, domain.ScrollLockStatusError, nil)
	}
}

func (sc *QueueManager) setStatus(cmd string, status domain.ScrollLockStatus, writeLock bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if value, ok := sc.commandQueue[cmd]; ok {
		value.Status = status
	}
	if writeLock {
		lock, err := sc.scrollService.GetLock()
		if err != nil {
			return
		}
		lock.SetStatus(cmd, status, nil)
	}
}

func (sc *QueueManager) GetQueue() map[string]domain.ScrollLockStatus {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	queue := make(map[string]domain.ScrollLockStatus)
	for cmd, item := range sc.commandQueue {
		queue[cmd] = item.Status
	}
	return queue
}
