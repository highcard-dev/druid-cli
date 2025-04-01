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
	fmt.Printf("NewQueueManager: Starting function\n")
	fmt.Printf("NewQueueManager: Creating new QueueManager instance\n")
	return &QueueManager{
		scrollService:    scrollService,
		processLauncher:  processLauncher,
		commandQueue:     make(map[string]*domain.QueueItem),
		taskChan:         make(chan string),
		taskDoneChan:     make(chan struct{}),
		shutdownChan:     make(chan struct{}),
		notifierChan:     make([]chan []string, 0),
		callbacksPostRun: make(map[string]func()),
	}
}

func (sc *QueueManager) workItem(cmd string) error {
	fmt.Printf("workItem: Starting function for command '%s'\n", cmd)
	queueItem := sc.GetQueueItem(cmd)
	fmt.Printf("workItem: Retrieved queue item: %+v\n", queueItem)
	if queueItem == nil {
		fmt.Printf("workItem: Queue item not found for command '%s'\n", cmd)
		return fmt.Errorf("command %s not found", cmd)
	}
	changeStatus := queueItem.UpdateLockStatus
	fmt.Printf("workItem: Got update lock status: %v\n", changeStatus)

	fmt.Printf("workItem: Logging debug information\n")
	logger.Log().Debug("Running command",
		zap.String("cmd", cmd),
		zap.Bool("changeStatus", changeStatus),
	)

	fmt.Printf("workItem: Calling process launcher to run command '%s'\n", cmd)
	return sc.processLauncher.Run(cmd, func(cmd string) error {
		fmt.Printf("workItem: Inside callback function with cmd: '%s'\n", cmd)
		return sc.AddTempItem(cmd)
	})
}

func (sc *QueueManager) notify() {
	fmt.Printf("notify: Starting function\n")
	queuedCommands := make([]string, 0)
	fmt.Printf("notify: Initialized empty queuedCommands slice\n")

	fmt.Printf("notify: Iterating through command queue\n")
	for cmd, _ := range sc.commandQueue {
		fmt.Printf("notify: Checking status for command '%s'\n", cmd)
		if sc.getStatus(cmd) != domain.ScrollLockStatusDone && sc.getStatus(cmd) != domain.ScrollLockStatusError {
			fmt.Printf("notify: Adding command '%s' to queuedCommands\n", cmd)
			queuedCommands = append(queuedCommands, cmd)
		}
	}

	fmt.Printf("notify: Iterating through notifier channels, count: %d\n", len(sc.notifierChan))
	for _, notifier := range sc.notifierChan {
		fmt.Printf("notify: Processing a notifier channel\n")
		select {
		case notifier <- queuedCommands:
			fmt.Printf("notify: Successfully sent queued commands to notifier\n")
			// Successfully sent queuedCommands to the notifier channel
		default:
			fmt.Printf("notify: Notifier channel not ready to receive\n")
			// The notifier channel is not ready to receive, handle accordingly
			// For example, log a warning or skip this notifier
		}
	}
	fmt.Printf("notify: Function completed\n")
}

func (sc *QueueManager) AddTempItem(cmd string) error {
	fmt.Printf("AddTempItem: Starting function for command '%s'\n", cmd)
	fmt.Printf("AddTempItem: Calling addQueueItem with Remember=false\n")
	return sc.addQueueItem(cmd, AddItemOptions{
		Remember: false,
	})
}

func (sc *QueueManager) AddAndRememberItem(cmd string) error {
	fmt.Printf("AddAndRememberItem: Starting function for command '%s'\n", cmd)
	fmt.Printf("AddAndRememberItem: Calling addQueueItem with Remember=true\n")
	return sc.addQueueItem(cmd, AddItemOptions{
		Remember: true,
	})
}

func (sc *QueueManager) AddShutdownItem(cmd string) error {
	fmt.Printf("AddShutdownItem: Starting function for command '%s'\n", cmd)
	fmt.Printf("AddShutdownItem: Calling addQueueItem with RunAfterExecution=Shutdown\n")
	return sc.addQueueItem(cmd, AddItemOptions{
		RunAfterExecution: func() {
			fmt.Printf("AddShutdownItem: Executing RunAfterExecution callback, calling Shutdown\n")
			sc.Shutdown()
		},
	})
}

func (sc *QueueManager) AddItemWithCallback(cmd string, cb func()) error {
	fmt.Printf("AddItemWithCallback: Starting function for command '%s'\n", cmd)
	fmt.Printf("AddItemWithCallback: Calling addQueueItem with custom callback\n")
	return sc.addQueueItem(cmd, AddItemOptions{
		RunAfterExecution: cb,
	})
}

func (sc *QueueManager) addQueueItem(cmd string, options AddItemOptions) error {
	fmt.Printf("addQueueItem: Starting function for command '%s'\n", cmd)
	fmt.Printf("addQueueItem: Acquiring mutex\n")
	sc.mu.Lock()
	fmt.Printf("addQueueItem: Setting up defer to unlock mutex\n")
	defer sc.mu.Unlock()

	fmt.Printf("addQueueItem: Initializing setLock with options.Remember: %v\n", options.Remember)
	setLock := options.Remember

	fmt.Printf("addQueueItem: Logging debug information\n")
	logger.Log().Debug("Running command",
		zap.String("cmd", cmd),
	)

	fmt.Printf("addQueueItem: Getting command details from scroll service\n")
	command, err := sc.scrollService.GetCommand(cmd)
	fmt.Printf("addQueueItem: Got command: %+v, error: %v\n", command, err)

	if err != nil {
		fmt.Printf("addQueueItem: Returning error: %v\n", err)
		return err
	}

	fmt.Printf("addQueueItem: Checking if command run mode is RunModeOnce\n")
	//Functions that run once, should be remembered, but should only have waiting status, when the are called explicitly
	if command.Run == domain.RunModeOnce {
		fmt.Printf("addQueueItem: Command is RunModeOnce, setting setLock=true\n")
		setLock = true
	}

	fmt.Printf("addQueueItem: Checking if command already in queue\n")
	if value, ok := sc.commandQueue[cmd]; ok {
		fmt.Printf("addQueueItem: Command found in queue with status: %v\n", value.Status)

		fmt.Printf("addQueueItem: Checking if command is already in progress\n")
		if value.Status != domain.ScrollLockStatusDone && value.Status != domain.ScrollLockStatusError {
			fmt.Printf("addQueueItem: Command already in queue and in progress, returning ErrAlreadyInQueue\n")
			return ErrAlreadyInQueue
		}

		fmt.Printf("addQueueItem: Checking if command is done and is RunModeOnce\n")
		if value.Status == domain.ScrollLockStatusDone && command.Run == domain.RunModeOnce {
			fmt.Printf("addQueueItem: Command already done and is RunModeOnce, returning ErrCommandDoneOnce\n")
			return ErrCommandDoneOnce
		}
	}

	fmt.Printf("addQueueItem: Creating new queue item\n")
	item := &domain.QueueItem{
		Status:           domain.ScrollLockStatusWaiting,
		UpdateLockStatus: setLock,
	}

	fmt.Printf("addQueueItem: Checking if RunAfterExecution callback provided\n")
	if options.RunAfterExecution != nil {
		fmt.Printf("addQueueItem: Setting RunAfterExecution callback\n")
		item.RunAfterExecution = options.RunAfterExecution
	}

	fmt.Printf("addQueueItem: Adding item to command queue\n")
	sc.commandQueue[cmd] = item

	fmt.Printf("addQueueItem: Checking if setLock is true\n")
	if setLock {
		fmt.Printf("addQueueItem: Getting lock from scroll service\n")
		lock, err := sc.scrollService.GetLock()
		fmt.Printf("addQueueItem: Got lock: %+v, error: %v\n", lock, err)
		if err != nil {
			fmt.Printf("addQueueItem: Error getting lock, returning error: %v\n", err)
			return err
		}
		fmt.Printf("addQueueItem: Setting status to WAITING in lock\n")
		lock.SetStatus(cmd, domain.ScrollLockStatusWaiting, nil)
	}
	fmt.Printf("addQueueItem: Sending command to taskChan\n")
	sc.taskChan <- cmd

	fmt.Printf("addQueueItem: Function completed successfully\n")
	return nil
}

func (sc *QueueManager) RegisterCallbacks(callbacks map[string]func()) {
	fmt.Printf("RegisterCallbacks: Starting function\n")
	fmt.Printf("RegisterCallbacks: Acquiring mutex\n")
	sc.mu.Lock()
	fmt.Printf("RegisterCallbacks: Setting up defer to unlock mutex\n")
	defer sc.mu.Unlock()

	fmt.Printf("RegisterCallbacks: Iterating through callbacks, count: %d\n", len(callbacks))
	for cmd, cb := range callbacks {
		fmt.Printf("RegisterCallbacks: Registering callback for command '%s'\n", cmd)
		sc.callbacksPostRun[cmd] = cb
	}
	fmt.Printf("RegisterCallbacks: Function completed\n")
}

func (sc *QueueManager) QueueLockFile() error {
	fmt.Printf("QueueLockFile: Starting method execution\n")
	lock, err := sc.scrollService.GetLock()
	fmt.Printf("QueueLockFile: Got lock with error: %v\n", err)

	if err != nil {
		fmt.Printf("QueueLockFile: Returning error: %v\n", err)
		return err
	}
	fmt.Printf("QueueLockFile: Starting to iterate through lock statuses, count: %d\n", len(lock.Statuses))
	for cmd, status := range lock.Statuses {
		fmt.Printf("QueueLockFile: Processing command '%s' with status: %+v\n", cmd, status)
		//convert legacy command names
		command, err := sc.scrollService.GetCommand(cmd)
		fmt.Printf("QueueLockFile: Retrieved command details: %+v, error: %v\n", command, err)
		if err != nil {
			fmt.Printf("QueueLockFile: Error getting command '%s': %v\n", cmd, err)
			return err
		}

		fmt.Printf("QueueLockFile: Checking if status is DONE for command '%s'\n", cmd)
		if status.Status == domain.ScrollLockStatusDone {
			fmt.Printf("QueueLockFile: Command '%s' status is DONE\n", cmd)
			//check callback
			fmt.Printf("QueueLockFile: Checking for callback for command '%s'\n", cmd)
			if callback, ok := sc.callbacksPostRun[cmd]; ok && callback != nil {
				fmt.Printf("QueueLockFile: Executing callback for command '%s'\n", cmd)
				callback()
				fmt.Printf("QueueLockFile: Finished executing callback for command '%s'\n", cmd)
			} else {
				fmt.Printf("QueueLockFile: No callback found for command '%s'\n", cmd)
			}

			fmt.Printf("QueueLockFile: Checking run mode for command '%s': %v\n", cmd, command.Run)
			//not sure if this can even happen for "restart", maybe on updates
			if command.Run != domain.RunModeRestart && command.Run != domain.RunModePersistent {
				fmt.Printf("QueueLockFile: Command '%s' is not restart/persistent type\n", cmd)

				//TODO: use addQueueItem here
				fmt.Printf("QueueLockFile: Acquiring mutex for command '%s'\n", cmd)
				sc.mu.Lock()
				fmt.Printf("QueueLockFile: Setting command '%s' as DONE in queue\n", cmd)
				sc.commandQueue[cmd] = &domain.QueueItem{
					Status:           domain.ScrollLockStatusDone,
					UpdateLockStatus: true,
				}
				fmt.Printf("QueueLockFile: Releasing mutex for command '%s'\n", cmd)
				sc.mu.Unlock()
				fmt.Printf("QueueLockFile: Continuing to next command\n")
				continue
			}
			fmt.Printf("QueueLockFile: Command '%s' is restart/persistent type\n", cmd)
		}
		fmt.Printf("QueueLockFile: Setting status to WAITING for command '%s'\n", cmd)
		status.Status = domain.ScrollLockStatusWaiting
		fmt.Printf("QueueLockFile: Calling addQueueItem for command '%s' with Remember=true\n", cmd)

		err = sc.addQueueItem(cmd, AddItemOptions{
			Remember: true,
		})
		fmt.Printf("QueueLockFile: Result of addQueueItem for command '%s': %v\n", cmd, err)
	}

	fmt.Printf("QueueLockFile: Method completed successfully\n")
	return nil
}

func (sc *QueueManager) Work() {
	fmt.Printf("Work: Starting function\n")

	fmt.Printf("Work: Entering infinite loop\n")
	for {
		fmt.Printf("Work: Waiting for channel events\n")
		select {
		case <-sc.taskChan:
			fmt.Printf("Work: Received event from taskChan\n")
			go (func() {
				fmt.Printf("Work: Starting goroutine for taskChan event\n")
				fmt.Printf("Work: Calling RunQueue\n")
				sc.RunQueue()
				fmt.Printf("Work: Calling notify\n")
				sc.notify()
				fmt.Printf("Work: Goroutine for taskChan event completed\n")
			})()
		case <-sc.taskDoneChan:
			fmt.Printf("Work: Received event from taskDoneChan\n")
			go (func() {
				fmt.Printf("Work: Starting goroutine for taskDoneChan event\n")
				fmt.Printf("Work: Calling RunQueue\n")
				sc.RunQueue()
				fmt.Printf("Work: Calling notify\n")
				sc.notify()
				fmt.Printf("Work: Goroutine for taskDoneChan event completed\n")
			})()
		case <-sc.shutdownChan:
			fmt.Printf("Work: Received shutdown signal\n")
			//empty queue
			fmt.Printf("Work: Clearing command queue\n")
			sc.commandQueue = make(map[string]*domain.QueueItem)
			fmt.Printf("Work: Function exiting due to shutdown\n")
			return
		}
	}
}

func (sc *QueueManager) RunQueue() {
	fmt.Printf("RunQueue: Starting function\n")
	fmt.Printf("RunQueue: Acquiring runQueueMu mutex\n")
	sc.runQueueMu.Lock()
	fmt.Printf("RunQueue: Setting up defer to unlock runQueueMu mutex\n")
	defer sc.runQueueMu.Unlock()

	fmt.Printf("RunQueue: Acquiring main mutex\n")
	sc.mu.Lock()

	fmt.Printf("RunQueue: Creating queueKeys map\n")
	queueKeys := make(map[string]domain.ScrollLockStatus, len(sc.commandQueue))
	fmt.Printf("RunQueue: Copying command queue statuses to queueKeys, count: %d\n", len(sc.commandQueue))
	for k, v := range sc.commandQueue {
		fmt.Printf("RunQueue: Copying command '%s' with status: %v\n", k, v.Status)
		queueKeys[k] = v.Status
	}

	fmt.Printf("RunQueue: Releasing main mutex\n")
	sc.mu.Unlock()

	fmt.Printf("RunQueue: Logging queue information\n")
	logger.Log().Info("Running queue", zap.Any("queueKeys", queueKeys))

	fmt.Printf("RunQueue: Iterating through queue keys, count: %d\n", len(queueKeys))
	for cmd, status := range queueKeys {
		fmt.Printf("RunQueue: Processing command '%s' with status: %v\n", cmd, status)

		fmt.Printf("RunQueue: Checking if command '%s' is already running\n", cmd)
		//if already running, skip
		if status == domain.ScrollLockStatusRunning {
			fmt.Printf("RunQueue: Command '%s' is already running, skipping\n", cmd)
			continue
		}

		fmt.Printf("RunQueue: Getting command details for '%s'\n", cmd)
		command, err := sc.scrollService.GetCommand(cmd)
		fmt.Printf("RunQueue: Got command: %+v, error: %v\n", command, err)
		if err != nil {
			fmt.Printf("RunQueue: Error getting command '%s', logging error and skipping\n", cmd)
			logger.Log().Error("Error getting command",
				zap.String("command", cmd),
				zap.Error(err),
			)
			continue
		}

		fmt.Printf("RunQueue: Checking if command '%s' is in error status\n", cmd)
		//if run Mode is restart, we need to run it again
		if status == domain.ScrollLockStatusError {
			fmt.Printf("RunQueue: Command '%s' is in error status, skipping\n", cmd)
			continue
		}

		fmt.Printf("RunQueue: Checking if command '%s' is done and not restart type\n", cmd)
		//if run Mode is restart, we need to run it again
		if status == domain.ScrollLockStatusDone && command.Run != domain.RunModeRestart {
			fmt.Printf("RunQueue: Command '%s' is done and not restart type, skipping\n", cmd)
			continue
		}

		fmt.Printf("RunQueue: Checking dependencies for command '%s'\n", cmd)
		dependencies := command.Needs
		fmt.Printf("RunQueue: Command '%s' has %d dependencies\n", cmd, len(dependencies))
		dependenciesReady := true
		for _, dep := range dependencies {
			fmt.Printf("RunQueue: Checking dependency '%s' for command '%s'\n", dep, cmd)
			_, ok := sc.commandQueue[dep]
			fmt.Printf("RunQueue: Dependency '%s' in queue: %v\n", dep, ok)
			//if item not in queue, add it and
			if !ok {
				fmt.Printf("RunQueue: Dependency '%s' not in queue, adding it\n", dep)
				dependenciesReady = false
				fmt.Printf("RunQueue: Calling AddTempItem for dependency '%s'\n", dep)
				sc.AddTempItem(dep)
				continue
			}

			fmt.Printf("RunQueue: Checking if dependency '%s' is done\n", dep)
			if sc.getStatus(dep) != domain.ScrollLockStatusDone {
				fmt.Printf("RunQueue: Dependency '%s' is not done, dependencies not ready\n", dep)
				dependenciesReady = false
				continue
			}
			fmt.Printf("RunQueue: Dependency '%s' is done\n", dep)
		}
		fmt.Printf("RunQueue: Dependencies ready for command '%s': %v\n", cmd, dependenciesReady)
		if dependenciesReady {
			fmt.Printf("RunQueue: Getting queue item for command '%s'\n", cmd)
			item := sc.GetQueueItem(cmd)
			fmt.Printf("RunQueue: Setting status to RUNNING for command '%s'\n", cmd)
			//we only run one process at a time, this is not optimal, but it is simple
			sc.setStatus(cmd, domain.ScrollLockStatusRunning, item.UpdateLockStatus)
			fmt.Printf("RunQueue: Logging command execution\n")
			logger.Log().Info("Running command", zap.String("command", cmd))
			fmt.Printf("RunQueue: Starting goroutine to execute command '%s'\n", cmd)
			go func(c string, i *domain.QueueItem) {
				fmt.Printf("RunQueue goroutine: Started for command '%s'\n", c)
				fmt.Printf("RunQueue goroutine: Setting up defer function\n")
				defer func() {
					fmt.Printf("RunQueue goroutine defer: Starting for command '%s'\n", c)
					fmt.Printf("RunQueue goroutine defer: Checking if RunAfterExecution callback exists\n")
					if i.RunAfterExecution != nil {
						fmt.Printf("RunQueue goroutine defer: Executing RunAfterExecution callback\n")
						i.RunAfterExecution()
					}
					fmt.Printf("RunQueue goroutine defer: Checking for post-run callback\n")
					if callback, ok := sc.callbacksPostRun[c]; ok && callback != nil {
						fmt.Printf("RunQueue goroutine defer: Executing post-run callback\n")
						callback()
					}

					fmt.Printf("RunQueue goroutine defer: Sending signal to taskDoneChan\n")
					sc.taskDoneChan <- struct{}{}
					fmt.Printf("RunQueue goroutine defer: Completed\n")
				}()
				fmt.Printf("RunQueue goroutine: Calling workItem for command '%s'\n", c)
				err := sc.workItem(c)
				fmt.Printf("RunQueue goroutine: workItem result for '%s': %v\n", c, err)
				if err != nil {
					fmt.Printf("RunQueue goroutine: Error running command '%s', setting status to ERROR\n", c)
					sc.setStatus(c, domain.ScrollLockStatusError, i.UpdateLockStatus)
					fmt.Printf("RunQueue goroutine: Logging error\n")
					logger.Log().Error("Error running command", zap.String("command", c), zap.Error(err))
					fmt.Printf("RunQueue goroutine: Returning due to error\n")
					return
				}

				fmt.Printf("RunQueue goroutine: Checking run mode for command '%s'\n", c)
				//restart means we are never done!
				if command.Run == domain.RunModeRestart {
					fmt.Printf("RunQueue goroutine: Command '%s' is restart type, setting status to WAITING\n", c)
					logger.Log().Info("Command done, restarting..", zap.String("command", c))
					sc.setStatus(c, domain.ScrollLockStatusWaiting, i.UpdateLockStatus)
				} else {
					fmt.Printf("RunQueue goroutine: Command '%s' completed, setting status to DONE\n", c)
					logger.Log().Info("Command done", zap.String("command", c))
					sc.setStatus(c, domain.ScrollLockStatusDone, i.UpdateLockStatus)
				}
				fmt.Printf("RunQueue goroutine: Completed for command '%s'\n", c)
			}(cmd, item)
		} else {
			fmt.Printf("RunQueue: Dependencies not ready for command '%s', logging info\n", cmd)
			logger.Log().Info("Dependencies not ready", zap.String("command", cmd))
		}
	}
	fmt.Printf("RunQueue: Function completed\n")
}

func (sc *QueueManager) Shutdown() {
	fmt.Printf("Shutdown: Starting function\n")
	fmt.Printf("Shutdown: Sending shutdown signal\n")
	sc.shutdownChan <- struct{}{}
	fmt.Printf("Shutdown: Function completed\n")
}

func (sc *QueueManager) WaitUntilEmpty() {
	fmt.Printf("WaitUntilEmpty: Starting function\n")
	fmt.Printf("WaitUntilEmpty: Creating notifier channel\n")
	notifier := make(chan []string)
	fmt.Printf("WaitUntilEmpty: Adding notifier to notifierChan slice\n")
	sc.notifierChan = append(sc.notifierChan, notifier)

	fmt.Printf("WaitUntilEmpty: Entering loop to wait for empty queue\n")
	for {
		fmt.Printf("WaitUntilEmpty: Waiting for notification\n")
		cmds := <-notifier
		fmt.Printf("WaitUntilEmpty: Received notification with %d commands\n", len(cmds))
		if len(cmds) == 0 {
			fmt.Printf("WaitUntilEmpty: Queue is empty, removing notifier\n")
			// remove notifier
			for i, n := range sc.notifierChan {
				fmt.Printf("WaitUntilEmpty: Checking notifier at index %d\n", i)
				if n == notifier {
					fmt.Printf("WaitUntilEmpty: Found notifier to remove at index %d\n", i)
					sc.notifierChan = append(sc.notifierChan[:i], sc.notifierChan[i+1:]...)
					fmt.Printf("WaitUntilEmpty: Removed notifier from slice\n")
					break
				}
			}
			fmt.Printf("WaitUntilEmpty: Function completed\n")
			return
		}
	}
}

func (sc *QueueManager) GetQueueItem(cmd string) *domain.QueueItem {
	fmt.Printf("GetQueueItem: Starting function for command '%s'\n", cmd)
	fmt.Printf("GetQueueItem: Acquiring mutex\n")
	sc.mu.Lock()
	fmt.Printf("GetQueueItem: Setting up defer to unlock mutex\n")
	defer sc.mu.Unlock()

	fmt.Printf("GetQueueItem: Checking if command '%s' exists in queue\n", cmd)
	if value, ok := sc.commandQueue[cmd]; ok {
		fmt.Printf("GetQueueItem: Found queue item for command '%s': %+v\n", cmd, value)
		return value
	}

	fmt.Printf("GetQueueItem: No queue item found for command '%s'\n", cmd)
	return nil
}

func (sc *QueueManager) getStatus(cmd string) domain.ScrollLockStatus {
	fmt.Printf("getStatus: Starting function for command '%s'\n", cmd)
	fmt.Printf("getStatus: Acquiring mutex\n")
	sc.mu.Lock()
	fmt.Printf("getStatus: Setting up defer to unlock mutex\n")
	defer sc.mu.Unlock()
	fmt.Printf("getStatus: Checking if command '%s' exists in queue\n", cmd)
	if value, ok := sc.commandQueue[cmd]; ok {
		fmt.Printf("getStatus: Found status for command '%s': %v\n", cmd, value.Status)
		return value.Status
	}
	fmt.Printf("getStatus: No status found for command '%s', returning DONE\n", cmd)
	return domain.ScrollLockStatusDone
}

func (sc *QueueManager) setStatus(cmd string, status domain.ScrollLockStatus, writeLock bool) {
	fmt.Printf("setStatus: Starting function for command '%s' with status %v, writeLock: %v\n", cmd, status, writeLock)
	fmt.Printf("setStatus: Acquiring mutex\n")
	sc.mu.Lock()
	fmt.Printf("setStatus: Setting up defer to unlock mutex\n")
	defer sc.mu.Unlock()
	fmt.Printf("setStatus: Checking if command '%s' exists in queue\n", cmd)
	if value, ok := sc.commandQueue[cmd]; ok {
		fmt.Printf("setStatus: Setting status to %v for command '%s'\n", status, cmd)
		value.Status = status
	}
	fmt.Printf("setStatus: Checking if writeLock is true\n")
	if writeLock {
		fmt.Printf("setStatus: Getting lock from scroll service\n")
		lock, err := sc.scrollService.GetLock()
		fmt.Printf("setStatus: Got lock with error: %v\n", err)
		if err != nil {
			fmt.Printf("setStatus: Error getting lock, returning\n")
			return
		}
		fmt.Printf("setStatus: Setting status in lock for command '%s'\n", cmd)
		lock.SetStatus(cmd, status, nil)
	}
	fmt.Printf("setStatus: Function completed\n")
}

func (sc *QueueManager) GetQueue() map[string]domain.ScrollLockStatus {
	fmt.Printf("GetQueue: Starting function\n")
	fmt.Printf("GetQueue: Acquiring mutex\n")
	sc.mu.Lock()
	fmt.Printf("GetQueue: Setting up defer to unlock mutex\n")
	defer sc.mu.Unlock()

	fmt.Printf("GetQueue: Creating result map\n")
	queue := make(map[string]domain.ScrollLockStatus)
	fmt.Printf("GetQueue: Copying command queue to result map, count: %d\n", len(sc.commandQueue))
	for cmd, item := range sc.commandQueue {
		fmt.Printf("GetQueue: Adding command '%s' with status %v to result map\n", cmd, item.Status)
		queue[cmd] = item.Status
	}
	fmt.Printf("GetQueue: Function completed with %d items\n", len(queue))
	return queue
}
