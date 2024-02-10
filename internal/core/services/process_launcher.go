package services

import (
	"errors"
	"fmt"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	logger "github.com/highcard-dev/daemon/internal/core/services/log"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"go.uber.org/zap"
)

type ProcessLauncher struct {
	pluginManager  *PluginManager
	processManager ports.ProcessManagerInterface
	ociRegistry    *registry.OciClient
	consoleManager ports.ConsoleManagerInterface
	logManager     ports.LogManagerInterface
	scrollService  ports.ScrollServiceInterface
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
	}

	return s
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
				cmd := domain.StreamCommand{
					Data:   item.Data,
					Stream: item.Stream,
				}
				sc.logManager.AddLine(item.Stream, cmd)

				consoles := sc.consoleManager.GetConsoles()
				//add console when stream is not found
				console, ok := consoles[item.Stream]
				if !ok {
					console = sc.consoleManager.AddConsoleWithChannel(item.Stream, "plugin", make(chan string))
				}
				console.Channel.Broadcast <- []byte(item.Data)
			}
		}
	}()

	scroll := sc.scrollService.GetFile()

	//init plugins
	return sc.pluginManager.ParseFromScroll(scroll.Plugins, sc.scrollService.GetScrollConfigRawYaml(), sc.scrollService.GetCwd())
}

func (sc *ProcessLauncher) Run(cmd string, processId string, changeStatus bool) error {
	logger.Log().LogRunCommand(processId, cmd)

	var command domain.CommandInstructionSet
	scroll := sc.scrollService.GetFile()

	//check if we can accually do it before we start
	if ps, ok := scroll.Processes[processId]; ok {
		cmds, ok := ps.Commands[cmd]
		if !ok {
			return errors.New("command " + cmd + " not found")
		}
		command = cmds
	} else {
		return errors.New("process " + processId + " not found")
	}

	if changeStatus && command.SchouldChangeStatus != "" {
		sc.scrollService.ChangeLockStatus(processId, command.SchouldChangeStatus)
	}
	for k, proc := range command.Procedures {
		logger.Log().LogRunProcedure(processId, cmd, k)
		_, err := sc.RunProcedure(proc, processId, changeStatus)
		if err != nil {
			return err
		}
	}

	return nil
}

func (sc *ProcessLauncher) RunProcedure(proc *domain.Procedure, processId string, changeStatus bool) (string, error) {
	processCwd := sc.scrollService.GetCwd()
	//check if we have a plugin for the mode
	if sc.pluginManager.HasMode(proc.Mode) {

		val, ok := proc.Data.(string)
		if !ok {
			return "", fmt.Errorf("invalid data type for plugin mode %s, expected data to be string but go %v", proc.Mode, proc.Data)
		}

		res, err := sc.pluginManager.RunProcedure(proc.Mode, val)
		return res, err
	}
	//check internal
	switch proc.Mode {
	//exec = create new process
	case "exec-tty":
		fallthrough
	case "exec":
		instructionsRaw, ok := proc.Data.([]interface{})
		if !ok {
			return "", errors.New("invalid instruction, expected array of strings")
		}

		// we have to manually []interface{} to []string :(
		instructions := make([]string, len(instructionsRaw))
		for i, v := range instructionsRaw {
			val, ok := v.(string)
			if !ok {
				return "", errors.New("invalid instruction, cannot convert to string")
			}
			instructions[i] = val
		}

		logger.Log().Debug("Running exec process",
			zap.String("processId", processId),
			zap.String("cwd", processCwd),
			zap.Strings("instructions", instructions),
		)
		var err error

		if proc.Mode == "exec-tty" {
			err = sc.processManager.RunTty(processId, instructions, processCwd)
		} else {
			err = sc.processManager.Run(processId, instructions, processCwd)
		}
		if err != nil {
			return "", err
		}
	case "stdin":

		logger.Log().Debug("Launching stdin process",
			zap.String("processId", processId),
			zap.String("cwd", processCwd),
			zap.String("instructions", proc.Data.(string)),
		)

		process := sc.processManager.GetRunningProcess(processId)
		if process == nil {
			return "", errors.New("process not found")
		}
		sc.processManager.WriteStdin(process, proc.Data.(string))

	case "command":

		logger.Log().Debug("Launching stdin process",
			zap.String("processId", processId),
			zap.String("cwd", processCwd),
			zap.String("instructions", proc.Data.(string)),
		)

		err := sc.Run(proc.Data.(string), processId, changeStatus)
		return "", err

	case "scroll-switch":

		logger.Log().Debug("Launching scroll-switch process",
			zap.String("processId", processId),
			zap.String("cwd", processCwd),
			zap.String("instructions", proc.Data.(string)),
		)

		err := sc.ociRegistry.Pull(sc.scrollService.GetDir(), proc.Data.(string))
		return "", err
	default:
		return "", errors.New("Unknown mode " + proc.Mode)
	}
	return "", nil
}

func (sc *ProcessLauncher) StartLockfile() error {

	scroll := sc.scrollService.GetFile()
	lock := sc.scrollService.GetLock()

	for process, status := range lock.Statuses {
		if status != "start" {
			continue
		}
		for cmdName, cmd := range scroll.Processes[process].Commands {
			if cmd.SchouldChangeStatus == "start" {
				logger.Log().Info("Running command",
					zap.String("commandName", cmdName),
				)
				go sc.Run(cmdName, process, true)
			}
		}
	}
	return nil
}

func (sc *ProcessLauncher) Initalize() error {
	lock := sc.scrollService.GetLock()
	scroll := sc.scrollService.GetCurrent()

	parts := strings.Split(scroll.Init, ".")

	if len(parts) != 2 {
		return errors.New("invalid init command")
	}
	initCommands := scroll.Processes[parts[0]].Commands[parts[1]].Procedures

	if len(initCommands) > 0 {
		go sc.Run(parts[1], parts[0], true)
		lock.Initialized = true
		lock.Write()
	}
	return nil
}
