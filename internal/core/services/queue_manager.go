package services

import (
	"fmt"
	"sync"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type QueueItem struct {
	status       domain.ScrollLockStatus
	changeStatus bool
}

type QueueManager struct {
	mu              sync.Mutex
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

func (sc *QueueManager) setCommandQueue(commandName string, status domain.ScrollLockStatus) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.commandQueue[commandName] = &QueueItem{
		status:       status,
		changeStatus: true,
	}
}

func (sc *QueueManager) workItem(cmd string, changeStatus bool) error {

	logger.Log().Debug("Running command",
		zap.String("cmd", cmd),
		zap.Bool("changeStatus", changeStatus),
	)

	command, err := sc.scrollService.GetCommand(cmd)

	if err != nil {
		return err
	}

	lock, err := sc.scrollService.GetLock()
	if err != nil {
		return err
	}

	sc.setCommandQueue(cmd, domain.ScrollLockStatusWaiting)
	if changeStatus {
		lock.SetStatus(cmd, domain.ScrollLockStatusWaiting)
	}

	status := lock.GetStatus(cmd)

	//Functions that run once, should be remembered, but should only have waiting status, when the are called explicitly
	if command.Run == domain.RunModeOnce {
		changeStatus = true
	}

	//if done and should be done once, skip
	if status == domain.ScrollLockStatusDone && command.Run == domain.RunModeOnce {
		sc.setCommandQueue(cmd, domain.ScrollLockStatusDone)
		return nil
	}

	sc.setCommandQueue(cmd, domain.ScrollLockStatusRunning)
	if changeStatus {
		lock.SetStatus(cmd, domain.ScrollLockStatusRunning)
	}

	err = sc.processLauncher.Run(cmd)

	if err != nil {
		sc.setCommandQueue(cmd, domain.ScrollLockStatusError)
		return err
	}

	//restart means we are never done!
	if changeStatus && command.Run != domain.RunModeRestart {
		lock.SetStatus(cmd, domain.ScrollLockStatusDone)
	}
	sc.setCommandQueue(cmd, domain.ScrollLockStatusDone)

	return nil

}

func (sc *QueueManager) notify() {
	queuedCommands := make([]string, 0)

	for cmd, item := range sc.commandQueue {
		if item.status != domain.ScrollLockStatusDone {
			queuedCommands = append(queuedCommands, cmd)
		}
	}

	for _, notifier := range sc.notifierChan {
		notifier <- queuedCommands
	}
}

func (sc *QueueManager) AddItem(cmd string, changeStatus bool) error {

	logger.Log().Debug("Running command",
		zap.String("cmd", cmd),
	)

	_, err := sc.scrollService.GetCommand(cmd)

	if err != nil {
		return err
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	if value, ok := sc.commandQueue[cmd]; ok {
		if value.status != domain.ScrollLockStatusDone {
			return fmt.Errorf("command %s is already in queue", cmd)
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
			changeStatus: false,
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
	for cmd, item := range sc.commandQueue {

		//if already running, skip
		if sc.commandQueue[cmd].status == domain.ScrollLockStatusRunning {
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
		if (item.status == domain.ScrollLockStatusError || item.status == domain.ScrollLockStatusDone) && command.Run != domain.RunModeRestart {
			continue
		}

		dependencies := command.Needs
		dependenciesReady := true
		for _, dep := range dependencies {
			childItem, ok := sc.commandQueue[dep]
			//if item not in queue, add it and
			if !ok {
				dependenciesReady = false
				sc.AddItem(dep, item.changeStatus)
				continue
			}

			if childItem.status != domain.ScrollLockStatusDone {
				dependenciesReady = false
				continue
			}
		}

		if dependenciesReady {
			err := sc.workItem(cmd, item.changeStatus)
			if err != nil {
				logger.Log().Error("Error running command", zap.String("command", cmd), zap.Error(err))
			}
			sc.taskDoneChan <- struct{}{}
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
			return
		}
	}
}
