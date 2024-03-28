package services

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ProcessLauncher struct {
	pluginManager  *PluginManager
	processManager ports.ProcessManagerInterface
	ociRegistry    *registry.OciClient
	consoleManager ports.ConsoleManagerInterface
	logManager     ports.LogManagerInterface
	scrollService  ports.ScrollServiceInterface
	commandQueue   map[string]domain.ScrollLockStatus
	mu             sync.Mutex
}

func NewProcessLauncher(
	ociRegistry *registry.OciClient,
	processManager ports.ProcessManagerInterface,
	pluginManager *PluginManager,
	consoleManager ports.ConsoleManagerInterface,
	logManager ports.LogManagerInterface,
	scrollService ports.ScrollServiceInterface,
) *ProcessLauncher {
	s := &ProcessLauncher{
		processManager: processManager,
		ociRegistry:    ociRegistry,
		pluginManager:  pluginManager,
		consoleManager: consoleManager,
		logManager:     logManager,
		scrollService:  scrollService,
		commandQueue:   make(map[string]domain.ScrollLockStatus),
	}

	return s
}

func (sc *ProcessLauncher) SetCommandQueue(commandName string, status domain.ScrollLockStatus) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.commandQueue[commandName] = status
}

func (sc *ProcessLauncher) RunNew(cmd string, processId string, changeStatus bool) error {

	command, err := sc.scrollService.GetCommand(cmd, processId)

	if err != nil {
		return err
	}

	name := processId + "." + cmd

	if value, ok := sc.commandQueue[name]; ok {
		if value != domain.ScrollLockStatusDone {
			return errors.New("command already in queue")
		}
	}

	sc.SetCommandQueue(name, domain.ScrollLockStatusWaiting)

	needs := command.Needs

	lock, err := sc.scrollService.GetLock()
	if err != nil {
		return err
	}

	if changeStatus {
		lock.SetStatus(processId, cmd, domain.ScrollLockStatusWaiting)
	}

	status := lock.GetStatus(processId, cmd)

	//Functions that run once, should be remembered, but should only have waiting status, when the are called explicitly
	if command.Run == domain.RunModeOnce {
		changeStatus = true
	}

	//if done and should be done once, skip
	if status == domain.ScrollLockStatusDone && command.Run == domain.RunModeOnce {
		sc.SetCommandQueue(name, domain.ScrollLockStatusDone)
		return nil
	}

	var wg sync.WaitGroup
	var runError error
	//check if needs are met, if not, start them, when not running, when running, wait
	for _, need := range needs {
		//check if is done in lockfile
		process, command := utils.ParseProcessAndCommand(need)
		if process == "" {
			sc.SetCommandQueue(name, domain.ScrollLockStatusError)
			return errors.New("invalid need " + need)
		}

		// else start
		wg.Add(1)
		go func(process string, command string) {
			defer wg.Done()

			need, err := sc.scrollService.GetCommand(command, process)

			if err != nil {
				runError = err
				logger.Log().Error("Error getting need",
					zap.String("process", process),
					zap.String("command", command),
					zap.Error(err),
				)

				sc.SetCommandQueue(name, domain.ScrollLockStatusError)
				return
			}

			if need.Run == domain.RunModeRestart {
				runError = errors.New("cannot have a need that is restart")
				logger.Log().Error("Error getting need",
					zap.String("process", process),
					zap.String("command", command),
					zap.Error(runError),
				)

				sc.SetCommandQueue(name, domain.ScrollLockStatusError)
				return
			}

			//Don't change status, for subtasks. Either it's the main task or a remebered subtask
			err = sc.RunNew(command, process, false)
			if err != nil {
				runError = err
				sc.SetCommandQueue(name, domain.ScrollLockStatusError)
			}
		}(process, command)
	}
	wg.Wait()

	sc.SetCommandQueue(name, domain.ScrollLockStatusRunning)

	if runError != nil {
		sc.SetCommandQueue(name, domain.ScrollLockStatusError)
		return runError
	}

	err = sc.Run(cmd, processId, changeStatus)
	if err != nil {
		sc.SetCommandQueue(name, domain.ScrollLockStatusError)
		return err
	}

	sc.SetCommandQueue(name, domain.ScrollLockStatusDone)

	return nil

}

// return at first
// TODO implement multiple scroll support
// To do this, best is to loop over activescrolldir and read every scroll
// TODO: remove initCommandsIdentifiers
func (sc *ProcessLauncher) LaunchPlugins() error {
	go func() {
		for {
			select {
			case item := <-sc.pluginManager.NotifyConsole:
				sc.logManager.AddLine(item.Stream, []byte(item.Data))

				consoles := sc.consoleManager.GetConsoles()
				//add console when stream is not found
				console, ok := consoles[item.Stream]
				if !ok {
					console = sc.consoleManager.AddConsoleWithChannel(item.Stream, domain.ConsoleTypePlugin, item.Stream, make(chan string))
				}
				console.Channel.Broadcast <- []byte(item.Data)
			}
		}
	}()

	scroll := sc.scrollService.GetFile()

	//init plugins
	return sc.pluginManager.ParseFromScroll(scroll.Plugins, string(sc.scrollService.GetScrollConfigRawYaml()), sc.scrollService.GetCwd())
}

func (sc *ProcessLauncher) Run(cmd string, processId string, changeStatus bool) error {

	command, err := sc.scrollService.GetCommand(cmd, processId)
	if err != nil {
		return err
	}

	lock, err := sc.scrollService.GetLock()
	if err != nil {
		return err
	}

	if changeStatus {
		lock.SetStatus(processId, cmd, domain.ScrollLockStatusRunning)
	}
	for _, proc := range command.Procedures {
		var err error
		var exitCode *int
		logger.Log().Debug("Running procedure",
			zap.String("processId", processId),
			zap.String("cmd", cmd),
			zap.String("mode", proc.Mode),
			zap.Any("data", proc.Data),
		)
		switch wait := proc.Wait.(type) {
		case int: //run in go routine and wait for x seconds
			go sc.RunProcedure(proc, processId, cmd)
			time.Sleep(time.Duration(wait) * time.Second)
		case bool: //run in go routine maybe wait
			if wait {
				_, exitCode, err = sc.RunProcedure(proc, processId, cmd)
			} else {
				go sc.RunProcedure(proc, processId, cmd)
			}
		default: //run and wait
			_, exitCode, err = sc.RunProcedure(proc, processId, cmd)
		}

		if err != nil {
			logger.Log().Error("Error running procedure",
				zap.String("processId", processId),
				zap.String("cmd", cmd),
				zap.Error(err))
			if changeStatus {
				lock.SetStatus(processId, cmd, domain.ScrollLockStatusError)
			}
			return err
		}

		if exitCode != nil && *exitCode != 0 {
			logger.Log().Error("Procedure ended with exit code "+fmt.Sprintf("%d", *exitCode),
				zap.String("processId", processId),
				zap.String("cmd", cmd),
				zap.Int("exitCode", *exitCode),
			)
			if changeStatus {
				lock.SetStatus(processId, cmd, domain.ScrollLockStatus(fmt.Sprintf("exit_code_%d", *exitCode)))
			}
			return fmt.Errorf("procedure %s failed with exit code %d", proc.Mode, *exitCode)
		}

		if exitCode == nil {
			logger.Log().Debug("Procedure ended")
		} else {
			logger.Log().Debug("Procedure ended with exit code 0")
		}
	}

	//restart means we are never done!
	if changeStatus && command.Run != domain.RunModeRestart {
		lock.SetStatus(processId, cmd, domain.ScrollLockStatusDone)
	}

	return nil
}

func (sc *ProcessLauncher) RunProcedure(proc *domain.Procedure, processId string, cmd string) (string, *int, error) {
	processCwd := sc.scrollService.GetCwd()
	//check if we have a plugin for the mode
	if sc.pluginManager.HasMode(proc.Mode) {

		val, ok := proc.Data.(string)
		if !ok {
			return "", nil, fmt.Errorf("invalid data type for plugin mode %s, expected data to be string but go %v", proc.Mode, proc.Data)
		}

		res, err := sc.pluginManager.RunProcedure(proc.Mode, val)
		logger.Log().Error("Error running plugin procedure", zap.Error(err))
		return res, nil, err
	}

	var err error
	//check internal
	switch proc.Mode {
	//exec = create new process
	case "exec-tty":
		fallthrough
	case "exec":
		var instructions []string
		instructions, err = utils.InterfaceToStringSlice(proc.Data)
		if err != nil {
			return "", nil, err
		}

		logger.Log().Debug("Running exec process",
			zap.String("processId", processId),
			zap.String("cwd", processCwd),
			zap.Strings("instructions", instructions),
		)
		var err error
		var exitCode *int

		if proc.Mode == "exec-tty" {
			exitCode, err = sc.processManager.RunTty(processId, cmd, instructions, processCwd)
		} else {
			exitCode, err = sc.processManager.Run(processId, cmd, instructions, processCwd)
		}
		return "", exitCode, err
	case "stdin":
		var instructions []string
		instructions, err = utils.InterfaceToStringSlice(proc.Data)
		if err != nil {
			return "", nil, err
		}

		if len(instructions) != 2 {
			return "", nil, errors.New("invalid stdin instructions")
		}
		processAndCommandToWriteTo := instructions[0]
		stdtIn := instructions[1]
		process1, command1 := utils.ParseProcessAndCommand(processAndCommandToWriteTo)

		logger.Log().Debug("Launching stdin process",
			zap.String("processId", processId),
			zap.String("cwd", processCwd),
			zap.Strings("instructions", instructions),
		)

		process := sc.processManager.GetRunningProcess(process1, command1)
		if process == nil {
			return "", nil, errors.New("process not found")
		}
		sc.processManager.WriteStdin(process, stdtIn)

	case "command":

		logger.Log().Debug("Launching stdin process",
			zap.String("processId", processId),
			zap.String("cwd", processCwd),
			zap.String("instructions", proc.Data.(string)),
		)

		err := sc.RunNew(proc.Data.(string), processId, false)
		return "", nil, err

	case "scroll-switch":

		logger.Log().Debug("Launching scroll-switch process",
			zap.String("processId", processId),
			zap.String("cwd", processCwd),
			zap.String("instructions", proc.Data.(string)),
		)

		err := sc.ociRegistry.Pull(sc.scrollService.GetDir(), proc.Data.(string))
		return "", nil, err
	default:
		return "", nil, errors.New("Unknown mode " + proc.Mode)
	}
	return "", nil, nil
}

func (sc *ProcessLauncher) StartLockfile(lock *domain.ScrollLock) error {

	for processAndCommand, status := range lock.Statuses {
		if status != domain.ScrollLockStatusRunning && status != domain.ScrollLockStatusWaiting && !strings.HasPrefix(string(status), "exit_code") {
			logger.Log().Debug("Skipping process", zap.String("processAndCommand", processAndCommand), zap.String("status", string(status)))
			continue
		}
		logger.Log().Info("Starting process from lockfile", zap.String("processAndCommand", processAndCommand), zap.String("status", string(status)))
		process, command := utils.ParseProcessAndCommand(processAndCommand)
		go sc.RunNew(command, process, true)
	}
	return nil
}

func (sc *ProcessLauncher) Initalize(lock *domain.ScrollLock) error {
	scroll := sc.scrollService.GetCurrent()

	parts := strings.Split(scroll.Init, ".")

	if _, ok := lock.Statuses[scroll.Init]; ok {
		//allready initialized
		return nil
	}

	if len(parts) != 2 {
		return errors.New("invalid init command")
	}
	initCommands := scroll.Processes[parts[0]].Commands[parts[1]].Procedures

	if len(initCommands) > 0 {
		go sc.RunNew(parts[1], parts[0], true)
		lock.Write()
	}
	return nil
}
