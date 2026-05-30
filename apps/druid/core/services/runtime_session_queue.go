package services

import (
	"context"
	"errors"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type runtimeQueueItem struct {
	err          error
	doneChan     chan struct{}
	inFlight     bool
	rememberDone bool
	runRequested bool
	restartCount uint
}

func (s *RuntimeSession) AddTempItem(cmd string) error {
	return s.addQueueItem(cmd, coreservices.AddItemOptions{})
}

func (s *RuntimeSession) AddForcedItem(cmd string) error {
	return s.addQueueItem(cmd, coreservices.AddItemOptions{Force: true})
}

func (s *RuntimeSession) AddTempItemWithWait(cmd string) error {
	return s.addQueueItem(cmd, coreservices.AddItemOptions{Wait: true})
}

func (s *RuntimeSession) RememberDoneItem(cmd string) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	item, ok := s.queue[cmd]
	if !ok {
		item = &runtimeQueueItem{}
		s.queue[cmd] = item
	}
	item.rememberDone = true
	item.inFlight = false
	item.err = nil
}

func (s *RuntimeSession) addQueueItem(cmd string, options coreservices.AddItemOptions) error {
	logger.Log().Debug("Running command", zap.String("cmd", cmd))

	command, err := s.scrollService.GetCommand(cmd)
	if err != nil {
		return err
	}

	s.queueMu.Lock()
	item := s.queue[cmd]
	snapshot := s.Snapshot()
	currentStatus, hasCurrentStatus := s.derivedScheduledStatusLocked(cmd, item, snapshot)
	if item != nil {
		if currentStatus != domain.ScrollLockStatusDone && currentStatus != domain.ScrollLockStatusError {
			s.queueMu.Unlock()
			return coreservices.ErrAlreadyInQueue
		}
	}
	if hasCurrentStatus && currentStatus == domain.ScrollLockStatusDone && command.Run == domain.RunModeOnce && !options.Force {
		s.queueMu.Unlock()
		return coreservices.ErrCommandDoneOnce
	}

	var doneChan chan struct{}
	if options.Wait {
		doneChan = make(chan struct{})
	}
	item = &runtimeQueueItem{doneChan: doneChan}
	s.queue[cmd] = item
	s.setQueueStatusLocked(cmd, item, domain.ScrollLockStatusWaiting, nil)
	s.queueMu.Unlock()

	s.triggerRunQueue()

	if options.Wait {
		<-doneChan
		s.queueMu.Lock()
		item := s.queue[cmd]
		var itemErr error
		if item != nil {
			itemErr = item.err
		}
		s.queueMu.Unlock()
		if itemErr != nil {
			return itemErr
		}
	}

	return nil
}

func (s *RuntimeSession) HydrateFromState(statuses domain.ProcedureStatusMap) error {
	for cmd, procedureStatuses := range statuses {
		command, err := s.scrollService.GetCommand(cmd)
		if err != nil {
			return err
		}

		commandStatus, ok := coreservices.DeriveCommandStatusFromProcedures(cmd, command, procedureStatuses)
		if !ok {
			continue
		}

		if commandStatus == domain.ScrollLockStatusDone {
			if command.Run != domain.RunModeRestart && command.Run != domain.RunModePersistent {
				s.queueMu.Lock()
				s.queue[cmd] = &runtimeQueueItem{rememberDone: true}
				s.queueMu.Unlock()
				continue
			}
		}

		s.rememberHydratedItem(cmd)
	}

	return nil
}

func (s *RuntimeSession) rememberHydratedItem(cmd string) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	item, ok := s.queue[cmd]
	if !ok {
		item = &runtimeQueueItem{}
		s.queue[cmd] = item
	}
	item.err = nil
	item.rememberDone = false
	item.runRequested = true
}

func (s *RuntimeSession) triggerRunQueue() {
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if !started {
		return
	}
	s.startRunQueue()
}

func (s *RuntimeSession) startRunQueue() {
	s.workWg.Add(1)
	go func() {
		defer s.workWg.Done()
		s.RunQueue()
		s.notify()
	}()
}

func (s *RuntimeSession) RunQueue() {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	s.queueMu.Lock()
	queueKeys := make(map[string]domain.ScrollLockStatus, len(s.queue))
	runRequested := make(map[string]bool, len(s.queue))
	snapshot := s.Snapshot()
	for cmd, item := range s.queue {
		status, _ := s.derivedScheduledStatusLocked(cmd, item, snapshot)
		queueKeys[cmd] = status
		runRequested[cmd] = item.runRequested
	}
	s.queueMu.Unlock()

	logger.Log().Info("Running queue", zap.Any("queueKeys", queueKeys))

	for cmd, status := range queueKeys {
		if status == domain.ScrollLockStatusRunning && !runRequested[cmd] {
			continue
		}

		command, err := s.scrollService.GetCommand(cmd)
		if err != nil {
			logger.Log().Error("Error getting command", zap.String("command", cmd), zap.Error(err))
			s.queueMu.Lock()
			delete(s.queue, cmd)
			s.queueMu.Unlock()
			continue
		}

		if status == domain.ScrollLockStatusError && !runRequested[cmd] {
			continue
		}

		isRestartMode := command.Run == domain.RunModeRestart
		if status == domain.ScrollLockStatusDone && !isRestartMode && !runRequested[cmd] {
			continue
		}

		dependenciesReady := true
		for _, dep := range command.Needs {
			if s.isScheduled(dep) {
				if s.getQueueStatus(dep) != domain.ScrollLockStatusDone {
					dependenciesReady = false
				}
				continue
			}

			dependencyStatus := s.derivedQueueStatus(dep)
			if dependencyStatus == domain.ScrollLockStatusDone {
				continue
			}

			dependenciesReady = false
			if err := s.AddTempItem(dep); err != nil && !errors.Is(err, coreservices.ErrAlreadyInQueue) && !errors.Is(err, coreservices.ErrCommandDoneOnce) {
				logger.Log().Error("Error adding dependency", zap.String("command", cmd), zap.String("dependency", dep), zap.Error(err))
			}
		}
		if !dependenciesReady {
			logger.Log().Info("Dependencies not ready", zap.String("command", cmd))
			continue
		}

		s.queueMu.Lock()
		item := s.queue[cmd]
		if item == nil {
			s.queueMu.Unlock()
			continue
		}
		runMode := command.Run
		s.setQueueStatusLocked(cmd, item, domain.ScrollLockStatusRunning, nil)
		s.queueMu.Unlock()

		logger.Log().Info("Running command", zap.String("command", cmd))
		s.workWg.Add(1)
		go func(c string, i *runtimeQueueItem) {
			defer s.workWg.Done()
			defer func() {
				if i.doneChan != nil {
					close(i.doneChan)
				}
				s.triggerRunQueue()
			}()

			startedAt := time.Now()
			err := s.runCommand(c)
			isRestartMode := runMode == domain.RunModeRestart

			if err != nil {
				logger.Log().Error("Error running command", zap.String("command", c), zap.Error(err))
				if !isRestartMode || domain.IsNonRetryableCommandError(err) {
					s.setQueueError(c, err)
					return
				}
			}

			if isRestartMode {
				s.setQueueStatus(c, domain.ScrollLockStatusWaiting, nil)
				s.queueMu.Lock()
				if time.Since(startedAt) < 30*time.Second {
					i.restartCount++
				} else {
					i.restartCount = 0
				}
				restartCount := i.restartCount
				s.queueMu.Unlock()
				if restartCount > 0 {
					backoff := time.Duration(1<<(restartCount-1)) * time.Second
					if backoff > 5*time.Minute {
						backoff = 5 * time.Minute
					}
					logger.Log().Info("Restarting with backoff", zap.String("command", c), zap.Duration("backoff", backoff), zap.Uint("restartCount", restartCount))
					time.Sleep(backoff)
				} else {
					logger.Log().Info("Command done, restarting", zap.String("command", c))
				}
			} else {
				logger.Log().Info("Command done", zap.String("command", c))
				s.setQueueStatus(c, domain.ScrollLockStatusDone, nil)
			}
		}(cmd, item)
	}
}

func (s *RuntimeSession) WaitUntilEmpty() {
	_ = s.WaitUntilEmptyContext(context.Background())
}

func (s *RuntimeSession) WaitUntilEmptyContext(ctx context.Context) error {
	notifier := make(chan []string, 10)

	s.queueMu.Lock()
	s.notifierChan = append(s.notifierChan, notifier)
	if !s.hasActiveItemsLocked() {
		s.removeNotifierLocked(notifier)
		s.queueMu.Unlock()
		return nil
	}
	s.queueMu.Unlock()
	defer func() {
		s.queueMu.Lock()
		s.removeNotifierLocked(notifier)
		s.queueMu.Unlock()
	}()

	for {
		select {
		case cmds := <-notifier:
			if len(cmds) == 0 {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *RuntimeSession) GetQueue() map[string]domain.ScrollLockStatus {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()

	queue := make(map[string]domain.ScrollLockStatus)
	snapshot := s.Snapshot()
	for cmd, item := range s.queue {
		queue[cmd], _ = s.derivedScheduledStatusLocked(cmd, item, snapshot)
	}
	return queue
}

func (s *RuntimeSession) stopDeploymentQueue() {
	s.mu.Lock()
	s.started = false
	s.mu.Unlock()
	s.drainQueueWork()
	s.resetQueueState()
}

func (s *RuntimeSession) drainQueueWork() {
	done := make(chan struct{})
	go func() {
		s.workWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		logger.Log().Warn("Timed out waiting for queue work to finish")
	}
}

func (s *RuntimeSession) resetQueueState() {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	s.queue = make(map[string]*runtimeQueueItem)
	s.notifierChan = make([]chan []string, 0)
}

func (s *RuntimeSession) notify() {
	s.queueMu.Lock()
	queuedCommands := make([]string, 0)
	snapshot := s.Snapshot()

	for cmd, item := range s.queue {
		status, _ := s.derivedScheduledStatusLocked(cmd, item, snapshot)
		if status != domain.ScrollLockStatusDone && status != domain.ScrollLockStatusError {
			queuedCommands = append(queuedCommands, cmd)
		}
	}

	notifiers := make([]chan []string, len(s.notifierChan))
	copy(notifiers, s.notifierChan)
	s.queueMu.Unlock()

	for _, notifier := range notifiers {
		select {
		case notifier <- queuedCommands:
		default:
			logger.Log().Debug("Skipping slow queue notifier")
		}
	}
}

func (s *RuntimeSession) isScheduled(cmd string) bool {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	_, ok := s.queue[cmd]
	return ok
}

func (s *RuntimeSession) getQueueStatus(cmd string) domain.ScrollLockStatus {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if item, ok := s.queue[cmd]; ok {
		status, _ := s.derivedScheduledStatusLocked(cmd, item, s.Snapshot())
		return status
	}
	return s.derivedQueueStatus(cmd)
}

func (s *RuntimeSession) derivedQueueStatus(cmd string) domain.ScrollLockStatus {
	command, err := s.scrollService.GetCommand(cmd)
	if err != nil {
		return domain.ScrollLockStatusWaiting
	}
	status, ok := coreservices.DeriveCommandStatusFromProcedures(cmd, command, s.Snapshot()[cmd])
	if !ok {
		return domain.ScrollLockStatusWaiting
	}
	return status
}

func (s *RuntimeSession) setQueueError(cmd string, err error) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if item, ok := s.queue[cmd]; ok {
		item.inFlight = false
		item.err = err
	}
	s.SetCommandStatus(cmd, domain.ScrollLockStatusError, coreservices.CommandExitCode(err))
}

func (s *RuntimeSession) setQueueStatus(cmd string, status domain.ScrollLockStatus, exitCode *int) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if item, ok := s.queue[cmd]; ok {
		s.setQueueStatusLocked(cmd, item, status, exitCode)
	}
}

func (s *RuntimeSession) setQueueStatusLocked(cmd string, item *runtimeQueueItem, status domain.ScrollLockStatus, exitCode *int) {
	item.inFlight = status == domain.ScrollLockStatusRunning
	if status != domain.ScrollLockStatusError {
		item.err = nil
	}
	switch status {
	case domain.ScrollLockStatusDone:
		item.rememberDone = true
		item.runRequested = false
	case domain.ScrollLockStatusWaiting:
		item.rememberDone = false
		item.runRequested = true
	case domain.ScrollLockStatusRunning:
		item.runRequested = false
	case domain.ScrollLockStatusError:
		item.runRequested = false
	}
	if status != domain.ScrollLockStatusRunning {
		s.SetCommandStatus(cmd, status, exitCode)
	}
}

func (s *RuntimeSession) derivedScheduledStatusLocked(cmd string, item *runtimeQueueItem, snapshot domain.ProcedureStatusMap) (domain.ScrollLockStatus, bool) {
	if item != nil {
		if item.err != nil {
			return domain.ScrollLockStatusError, true
		}
		if item.inFlight {
			return domain.ScrollLockStatusRunning, true
		}
		if item.rememberDone {
			return domain.ScrollLockStatusDone, true
		}
	}
	command, err := s.scrollService.GetCommand(cmd)
	if err != nil {
		return domain.ScrollLockStatusWaiting, item != nil
	}
	if status, ok := coreservices.DeriveCommandStatusFromProcedures(cmd, command, snapshot[cmd]); ok {
		return status, true
	}
	if item != nil {
		return domain.ScrollLockStatusWaiting, true
	}
	return "", false
}

func (s *RuntimeSession) hasActiveItemsLocked() bool {
	snapshot := s.Snapshot()
	for cmd, item := range s.queue {
		status, _ := s.derivedScheduledStatusLocked(cmd, item, snapshot)
		if status != domain.ScrollLockStatusDone && status != domain.ScrollLockStatusError {
			return true
		}
	}
	return false
}

func (s *RuntimeSession) removeNotifierLocked(notifier chan []string) {
	for i, n := range s.notifierChan {
		if n == notifier {
			s.notifierChan = append(s.notifierChan[:i], s.notifierChan[i+1:]...)
			return
		}
	}
}
