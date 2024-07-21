package services

import (
	"errors"
	"fmt"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ProcedureLauncher struct {
	pluginManager  ports.PluginManagerInterface
	processManager ports.ProcessManagerInterface
	ociRegistry    ports.OciRegistryInterface
	consoleManager ports.ConsoleManagerInterface
	logManager     ports.LogManagerInterface
	scrollService  ports.ScrollServiceInterface
}

func NewProcedureLauncher(
	ociRegistry ports.OciRegistryInterface,
	processManager ports.ProcessManagerInterface,
	pluginManager ports.PluginManagerInterface,
	consoleManager ports.ConsoleManagerInterface,
	logManager ports.LogManagerInterface,
	scrollService ports.ScrollServiceInterface,
) *ProcedureLauncher {
	s := &ProcedureLauncher{
		processManager: processManager,
		ociRegistry:    ociRegistry,
		pluginManager:  pluginManager,
		consoleManager: consoleManager,
		logManager:     logManager,
		scrollService:  scrollService,
	}

	return s
}

func (sc *ProcedureLauncher) LaunchPlugins() error {
	go func() {
		for {
			select {
			case item := <-sc.pluginManager.GetNotifyConsoleChannel():
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

// I am unsure if we should support he command mode in the future as it is an antipattern for the scroll architecture, we try to solve stuff with dependencies
func (sc *ProcedureLauncher) Run(cmd string, runCommandCb func(cmd string) error) error {

	command, err := sc.scrollService.GetCommand(cmd)
	if err != nil {
		return err
	}

	for _, proc := range command.Procedures {

		if proc.Mode == "command" {
			if proc.Wait != nil {
				return errors.New("command mode does not support wait")
			}
			err = runCommandCb(proc.Data.(string))
			return err
		}

		var err error
		var exitCode *int
		logger.Log().Debug("Running procedure",
			zap.String("cmd", cmd),
			zap.String("mode", proc.Mode),
			zap.Any("data", proc.Data),
		)
		switch wait := proc.Wait.(type) {
		case int: //run in go routine and wait for x seconds
			go func(procedure domain.Procedure) {
				time.Sleep(time.Duration(wait) * time.Second)
				sc.RunProcedure(&procedure, cmd)
			}(*proc)
		case bool: //run in go routine maybe wait
			if wait {
				_, exitCode, err = sc.RunProcedure(proc, cmd)
			} else {
				go sc.RunProcedure(proc, cmd)
			}
		default: //run and wait
			_, exitCode, err = sc.RunProcedure(proc, cmd)
		}

		if err != nil {
			logger.Log().Error("Error running procedure",
				zap.String("cmd", cmd),
				zap.Error(err))
			return err
		}

		if exitCode != nil && *exitCode != 0 {
			logger.Log().Error("Procedure ended with exit code "+fmt.Sprintf("%d", *exitCode),
				zap.String("cmd", cmd),
				zap.Int("exitCode", *exitCode),
			)
			return fmt.Errorf("procedure %s failed with exit code %d", proc.Mode, *exitCode)
		}

		if exitCode == nil {
			logger.Log().Debug("Procedure ended")
		} else {
			logger.Log().Debug("Procedure ended with exit code 0")
		}
	}

	return nil
}

func (sc *ProcedureLauncher) RunProcedure(proc *domain.Procedure, cmd string) (string, *int, error) {

	logger.Log().Debug("Running procedure",
		zap.String("cmd", cmd),
		zap.String("mode", proc.Mode),
		zap.Any("data", proc.Data),
	)

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
			zap.String("cwd", processCwd),
			zap.Strings("instructions", instructions),
		)
		var err error
		var exitCode *int

		if proc.Mode == "exec-tty" {
			exitCode, err = sc.processManager.RunTty(cmd, instructions, processCwd)
		} else {
			exitCode, err = sc.processManager.Run(cmd, instructions, processCwd)
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
		commandToWriteTo := instructions[0]
		stdtIn := instructions[1]

		logger.Log().Debug("Launching stdin process",
			zap.String("cwd", processCwd),
			zap.Strings("instructions", instructions),
		)

		process := sc.processManager.GetRunningProcess(commandToWriteTo)
		if process == nil {
			return "", nil, errors.New("process not found")
		}
		sc.processManager.WriteStdin(process, stdtIn)

	case "scroll-switch":

		logger.Log().Debug("Launching scroll-switch process",
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
