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

type QueueItem struct {
	status       domain.ScrollLockStatus
	changeStatus bool
}

type QueueManager struct {
	mu              sync.Mutex
	runQueueMu      sync.Mutex
	scrollService   ports.ScrollServiceInterface
	processLauncher ports.ProcedureLauchnerInterface
	commandQueue    map[string]*QueueItem
	taskChan        chan string
	taskDoneChan    chan struct{}
	shutdownChan    chan struct{}
	notifierChan    []chan []string
}

func NewQueueManager(
	scrollService ports.ScrollServiceInterface,
	processLauncher ports.ProcedureLauchnerInterface,
) *QueueManager {
	return &QueueManager{
		scrollService:   scrollService,
		processLauncher: processLauncher,
		commandQueue:    make(map[string]*QueueItem),
		taskChan:        make(chan string),
		taskDoneChan:    make(chan struct{}),
		shutdownChan:    make(chan struct{}),
		notifierChan:    make([]chan []string, 0),
	}
}

func (sc *QueueManager) workItem(cmd string) error {

	queueItem := sc.GetQueueItem(cmd)
	if queueItem == nil {
		return fmt.Errorf("command %s not found", cmd)
	}
	changeStatus := queueItem.changeStatus

	logger.Log().Debug("Running command",
		zap.String("cmd", cmd),
		zap.Bool("changeStatus", changeStatus),
	)

	return sc.processLauncher.Run(cmd, func(cmd string) error {
		return sc.AddItem(cmd, changeStatus)
	})
}

func (sc *QueueManager) notify() {
	queuedCommands := make([]string, 0)

	for cmd, _ := range sc.commandQueue {
		if sc.getStatus(cmd) != domain.ScrollLockStatusDone && sc.getStatus(cmd) != domain.ScrollLockStatusError {
			queuedCommands = append(queuedCommands, cmd)
		}
	}

	for _, notifier := range sc.notifierChan {
		select {
		case notifier <- queuedCommands:
			// Successfully sent queuedCommands to the notifier channel
		default:
			// The notifier channel is not ready to receive, handle accordingly
			// For example, log a warning or skip this notifier
		}
	}
}

func (sc *QueueManager) AddItem(cmd string, changeStatus bool) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	logger.Log().Debug("Running command",
		zap.String("cmd", cmd),
	)

	command, err := sc.scrollService.GetCommand(cmd)

	if err != nil {
		return err
	}

	//Functions that run once, should be remembered, but should only have waiting status, when the are called explicitly
	if command.Run == domain.RunModeOnce {
		changeStatus = true
	}

	if value, ok := sc.commandQueue[cmd]; ok {

		if value.status != domain.ScrollLockStatusDone && value.status != domain.ScrollLockStatusError {
			return ErrAlreadyInQueue
		}

		if value.status == domain.ScrollLockStatusDone && command.Run == domain.RunModeOnce {
			return ErrCommandDoneOnce
		}
	}

	sc.commandQueue[cmd] = &QueueItem{
		status:       domain.ScrollLockStatusWaiting,
		changeStatus: changeStatus,
	}
	sc.taskChan <- cmd

	return nil
}

func (sc *QueueManager) QueueLockFile() error {
	lock, err := sc.scrollService.GetLock()

	if err != nil {
		return err
	}

	for cmd, status := range lock.Statuses {
		sc.commandQueue[cmd] = &QueueItem{
			status:       status,
			changeStatus: true,
		}
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
			//todo cleanup
			return
		}
	}
}

func (sc *QueueManager) RunQueue() {
	sc.runQueueMu.Lock()
	defer sc.runQueueMu.Unlock()

	for cmd, item := range sc.commandQueue {

		//if already running, skip
		if sc.getStatus(cmd) == domain.ScrollLockStatusRunning {
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
		if (sc.getStatus(cmd) == domain.ScrollLockStatusError || sc.getStatus(cmd) == domain.ScrollLockStatusDone) && command.Run != domain.RunModeRestart {
			continue
		}

		dependencies := command.Needs
		dependenciesReady := true
		for _, dep := range dependencies {
			_, ok := sc.commandQueue[dep]
			//if item not in queue, add it and
			if !ok {
				dependenciesReady = false
				sc.AddItem(dep, item.changeStatus)
				continue
			}

			if sc.getStatus(dep) != domain.ScrollLockStatusDone {
				dependenciesReady = false
				continue
			}
		}

		if dependenciesReady {
			//we only run one process at a time, this is not optimal, but it is simple
			sc.setStatus(cmd, domain.ScrollLockStatusRunning, item.changeStatus)
			go func(c string, i *QueueItem) {

				err := sc.workItem(c)
				if err != nil {
					sc.setStatus(c, domain.ScrollLockStatusError, i.changeStatus)
					logger.Log().Error("Error running command", zap.String("command", c), zap.Error(err))
					sc.taskDoneChan <- struct{}{}
					return
				}

				//restart means we are never done!
				if i.changeStatus && command.Run != domain.RunModeRestart {
					sc.setStatus(c, domain.ScrollLockStatusDone, true)
				} else {
					if command.Run == domain.RunModeRestart {
						sc.setStatus(c, domain.ScrollLockStatusWaiting, false)
					} else {
						sc.setStatus(c, domain.ScrollLockStatusDone, false)
					}
				}
				sc.taskDoneChan <- struct{}{}
			}(cmd, item)
		}
	}
}

func (sc *QueueManager) Shutdown() {
	sc.shutdownChan <- struct{}{}
}

func (sc *QueueManager) WaitUntilEmpty() {
	notifier := make(chan []string)
	sc.notifierChan = append(sc.notifierChan, notifier)

	for {
		cmds := <-notifier
		if len(cmds) == 0 {
			// remove notifier
			for i, n := range sc.notifierChan {
				if n == notifier {
					sc.notifierChan = append(sc.notifierChan[:i], sc.notifierChan[i+1:]...)
					break
				}
			}
			return
		}
	}
}

func (sc *QueueManager) GetQueueItem(cmd string) *QueueItem {
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
		return value.status
	}
	return domain.ScrollLockStatusDone
}

func (sc *QueueManager) setStatus(cmd string, status domain.ScrollLockStatus, writeLock bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if value, ok := sc.commandQueue[cmd]; ok {
		value.status = status
	}
	if writeLock {
		lock, err := sc.scrollService.GetLock()
		if err != nil {
			return
		}
		lock.SetStatus(cmd, status)
	}
}