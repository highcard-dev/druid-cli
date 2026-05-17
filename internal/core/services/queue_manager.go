package services

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

var ErrAlreadyInQueue = fmt.Errorf("command is already in queue")
var ErrCommandNotFound = fmt.Errorf("command not found")
var ErrCommandDoneOnce = fmt.Errorf("command is already done and has run mode once")

type AddItemOptions struct {
	Wait  bool
	Force bool
}

type QueueStatusObserver func(command string, status domain.ScrollLockStatus, exitCode *int)

type QueueManager struct {
	mu                sync.Mutex
	runQueueMu        sync.Mutex
	scrollService     ports.ScrollServiceInterface
	procedureLauncher ports.ProcedureLauchnerInterface
	commandQueue      map[string]*domain.QueueItem
	taskChan          chan string
	taskDoneChan      chan struct{}
	shutdownChan      chan struct{}
	notifierChan      []chan []string
	statusObserver    QueueStatusObserver
}

func NewQueueManager(
	scrollService ports.ScrollServiceInterface,
	procedureLauncher ports.ProcedureLauchnerInterface,
) *QueueManager {
	return &QueueManager{
		scrollService:     scrollService,
		procedureLauncher: procedureLauncher,
		commandQueue:      make(map[string]*domain.QueueItem),
		taskChan:          make(chan string, 100), // FIXED: Buffered channel
		taskDoneChan:      make(chan struct{}, 1), // FIXED: Buffered channel
		shutdownChan:      make(chan struct{}),
		notifierChan:      make([]chan []string, 0),
	}
}

func (sc *QueueManager) workItem(cmd string) error {
	queueItem := sc.GetQueueItem(cmd)
	if queueItem == nil {
		return fmt.Errorf("command %s not found", cmd)
	}

	logger.Log().Debug("Running command",
		zap.String("cmd", cmd),
	)

	return sc.procedureLauncher.Run(cmd)
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
	return sc.addQueueItem(cmd, AddItemOptions{})
}

func (sc *QueueManager) AddForcedItem(cmd string) error {
	return sc.addQueueItem(cmd, AddItemOptions{Force: true})
}

func (sc *QueueManager) RememberDoneItem(cmd string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if _, ok := sc.commandQueue[cmd]; ok {
		return
	}
	sc.commandQueue[cmd] = &domain.QueueItem{
		Status: domain.ScrollLockStatusDone,
	}
}

func (sc *QueueManager) AddTempItemWithWait(cmd string) error {
	return sc.addQueueItem(cmd, AddItemOptions{
		Wait: true,
	})
}

func (sc *QueueManager) addQueueItem(cmd string, options AddItemOptions) error {
	sc.mu.Lock()

	logger.Log().Debug("Running command",
		zap.String("cmd", cmd),
	)

	command, err := sc.scrollService.GetCommand(cmd)

	if err != nil {
		sc.mu.Unlock()
		return err
	}

	if value, ok := sc.commandQueue[cmd]; ok {

		if value.Status != domain.ScrollLockStatusDone && value.Status != domain.ScrollLockStatusError {
			sc.mu.Unlock()
			return ErrAlreadyInQueue
		}

		if value.Status == domain.ScrollLockStatusDone && command.Run == domain.RunModeOnce && !options.Force {
			sc.mu.Unlock()
			return ErrCommandDoneOnce
		}
	}

	var doneChan chan struct{}
	if options.Wait {
		doneChan = make(chan struct{})
	}

	item := &domain.QueueItem{
		Status:   domain.ScrollLockStatusWaiting,
		DoneChan: doneChan,
	}

	sc.commandQueue[cmd] = item
	sc.observeStatusLocked(cmd, domain.ScrollLockStatusWaiting, nil)

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

func (sc *QueueManager) SetStatusObserver(observer QueueStatusObserver) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.statusObserver = observer
}

func (sc *QueueManager) HydrateCommandStatuses(statuses map[string]domain.LockStatus) error {
	for cmd, status := range statuses {
		command, err := sc.scrollService.GetCommand(cmd)
		if err != nil {
			return err
		}

		if status.Status == domain.ScrollLockStatusDone {
			if command.Run != domain.RunModeRestart && command.Run != domain.RunModePersistent {
				sc.mu.Lock()
				sc.commandQueue[cmd] = &domain.QueueItem{
					Status: domain.ScrollLockStatusDone,
				}
				sc.mu.Unlock()
				continue
			}
		}

		sc.addQueueItem(cmd, AddItemOptions{})
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
			sc.mu.Lock()
			delete(sc.commandQueue, cmd)
			sc.mu.Unlock()
			continue
		}

		//if in error state and not a restart/persistent mode, skip
		if status == domain.ScrollLockStatusError {
			continue
		}

		//if done and not a restart/persistent mode, skip
		isRestartMode := command.Run == domain.RunModeRestart
		if status == domain.ScrollLockStatusDone && !isRestartMode {
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
			runMode := command.Run
			// We only run one command at a time to keep dependency resolution deterministic.
			sc.setStatus(cmd, domain.ScrollLockStatusRunning, nil)
			logger.Log().Info("Running command", zap.String("command", cmd))
			go func(c string, i *domain.QueueItem) {
				defer func() {
					// Signal completion if someone is waiting
					if i.DoneChan != nil {
						close(i.DoneChan)
					}

					// FIXED: Non-blocking send to buffered channel
					sc.taskDoneChan <- struct{}{}
				}()

				startedAt := time.Now()
				err := sc.workItem(c)
				isRestartMode := runMode == domain.RunModeRestart

				if err != nil {
					logger.Log().Error("Error running command", zap.String("command", c), zap.Error(err))
					if !isRestartMode || domain.IsNonRetryableCommandError(err) {
						sc.setError(c, err)
						return
					}
				}

				if isRestartMode {
					// Set status to waiting immediately so shutdown captures correct state.
					sc.setStatus(c, domain.ScrollLockStatusWaiting, nil)

					// Exponential backoff for fast restarts (1s, 2s, 4s, ... max 5m)
					if time.Since(startedAt) < 30*time.Second {
						i.RestartCount++
					} else {
						i.RestartCount = 0
					}
					if i.RestartCount > 0 {
						backoff := time.Duration(1<<(i.RestartCount-1)) * time.Second
						if backoff > 5*time.Minute {
							backoff = 5 * time.Minute
						}
						logger.Log().Info("Restarting with backoff", zap.String("command", c), zap.Duration("backoff", backoff), zap.Uint("restartCount", i.RestartCount))
						time.Sleep(backoff)
					} else {
						logger.Log().Info("Command done, restarting..", zap.String("command", c))
					}
				} else {
					logger.Log().Info("Command done", zap.String("command", c))
					sc.setStatus(c, domain.ScrollLockStatusDone, nil)
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
	if !sc.hasActiveItemsLocked() {
		sc.removeNotifierLocked(notifier)
		sc.mu.Unlock()
		return
	}
	sc.mu.Unlock()

	for {

		cmds := <-notifier
		if len(cmds) == 0 {
			// remove notifier
			sc.mu.Lock()
			sc.removeNotifierLocked(notifier)
			sc.mu.Unlock()
			return
		}
	}

}

func (sc *QueueManager) hasActiveItemsLocked() bool {
	for _, item := range sc.commandQueue {
		if item.Status != domain.ScrollLockStatusDone && item.Status != domain.ScrollLockStatusError {
			return true
		}
	}
	return false
}

func (sc *QueueManager) removeNotifierLocked(notifier chan []string) {
	for i, n := range sc.notifierChan {
		if n == notifier {
			sc.notifierChan = append(sc.notifierChan[:i], sc.notifierChan[i+1:]...)
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

func (sc *QueueManager) setError(cmd string, err error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if value, ok := sc.commandQueue[cmd]; ok {
		value.Status = domain.ScrollLockStatusError
		value.Error = err
	}
	sc.observeStatusLocked(cmd, domain.ScrollLockStatusError, commandExitCode(err))
}

func (sc *QueueManager) setStatus(cmd string, status domain.ScrollLockStatus, exitCode *int) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if value, ok := sc.commandQueue[cmd]; ok {
		value.Status = status
	}
	sc.observeStatusLocked(cmd, status, exitCode)
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

func (sc *QueueManager) observeStatusLocked(cmd string, status domain.ScrollLockStatus, exitCode *int) {
	if sc.statusObserver == nil {
		return
	}
	sc.statusObserver(cmd, status, exitCode)
}

func commandExitCode(err error) *int {
	var commandErr *domain.CommandExecutionError
	if err != nil && errors.As(err, &commandErr) {
		return &commandErr.ExitCode
	}
	return nil
}
