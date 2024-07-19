package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/creack/pty"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ProcessManager struct {
	runningProcesses map[string]*domain.Process
	logManager       ports.LogManagerInterface
	consoleManager   ports.ConsoleManagerInterface
	processMonitor   ports.ProcessMonitorInterface
}

func NewProcessManager(logManager ports.LogManagerInterface, consoleManager ports.ConsoleManagerInterface, processMonitor ports.ProcessMonitorInterface) *ProcessManager {
	return &ProcessManager{
		runningProcesses: make(map[string]*domain.Process),
		logManager:       logManager,
		consoleManager:   consoleManager,
		processMonitor:   processMonitor,
	}
}

func (po *ProcessManager) RunTty(commandName string, command []string, cwd string) (*int, error) {

	process := domain.Process{
		Name: commandName,
		Type: "process_tty",
	}

	if process.Cmd != nil {
		return nil, errors.New("process already running")
	}

	name, args := command[0], command[1:]

	logger.Log().Debug("LaunchTty",
		zap.String("processName", name),
		zap.Strings("args", args),
		zap.String("dir", cwd),
	)

	process.Cmd = exec.Command(name, args...)
	process.Cmd.Dir = cwd

	logger.Log().Info("Starting tty process", zap.String("commandName", commandName), zap.String("name", name), zap.Strings("args", args), zap.String("dir", cwd))

	out, err := pty.Start(process.Cmd)
	if err != nil {
		return nil, err
	}

	process.StdIn = out

	//self register process
	po.AddRunningProcess(commandName, &process)

	//add process for monitoring
	po.processMonitor.AddProcess(int32(process.Cmd.Process.Pid), commandName)

	//slight difference to normal process, as we only attach after the process has started
	//add console output

	prefixedProcessCommandName := fmt.Sprintf("process_tty.%s", commandName)

	po.consoleManager.AddConsoleWithIoReader(prefixedProcessCommandName, domain.ConsoleTypeTTY, "stdin", out)

	//reset periodically
	process.Cmd.Wait()
	po.processMonitor.RemoveProcess(commandName)
	po.RemoveProcess(commandName)

	// Wait for goroutine to print everything (watchdog closes stdin)
	exitCode := process.Cmd.ProcessState.ExitCode()
	po.consoleManager.MarkExited(prefixedProcessCommandName, exitCode)

	return &exitCode, nil
}

func (po *ProcessManager) Run(commandName string, command []string, dir string) (*int, error) {

	process := domain.Process{
		Name: commandName,
		Type: "process",
	}
	//Todo, add processmonitoring explicitly here
	if process.Cmd != nil {
		return nil, errors.New("process already running")
	}

	cmdCtx, cmdDone := context.WithCancel(context.Background())

	//Split command to slice
	name, args := command[0], command[1:]

	logger.Log().Debug("Launch",
		zap.String("commandName", commandName),
		zap.String("name", name),
		zap.Strings("args", args),
		zap.String("dir", dir),
	)

	process.Cmd = exec.Command(name, args...)
	process.Cmd.Dir = dir

	stdoutReader, err := process.Cmd.StdoutPipe()
	if err != nil {
		cmdDone()
		return nil, err
	}

	stderrReader, err := process.Cmd.StderrPipe()
	if err != nil {
		cmdDone()
		return nil, err
	}

	stdin, err := process.Cmd.StdinPipe()

	if err != nil {
		cmdDone()
		return nil, err
	}

	process.StdIn = stdin

	prefixedProcessCommandName := fmt.Sprintf("process.%s", commandName)

	po.consoleManager.AddConsoleWithIoReader(prefixedProcessCommandName, domain.ConsoleTypeProcess, "stdin", stdoutReader)
	po.consoleManager.AddConsoleWithIoReader(prefixedProcessCommandName, domain.ConsoleTypeProcess, "stdin", stderrReader)

	// Run and wait for Cmd to return, discard Status
	err = process.Cmd.Start()

	if err != nil {
		cmdDone()
		process.Cmd = nil
		return nil, err
	}

	//self register process
	po.AddRunningProcess(commandName, &process)

	//add process for monitoring
	po.processMonitor.AddProcess(int32(process.Cmd.Process.Pid), commandName)

	//add console output

	//WARNING MultiReader is not working as expected, it seems to block the process and process.Wait() never returns
	//stdReader := io.MultiReader(stdoutReader, stderrReader)

	go func() {
		_ = process.Cmd.Wait()
		cmdDone()

		stderrReader.Close()
		stdoutReader.Close()
		stdin.Close()
	}()

	<-cmdCtx.Done()

	po.processMonitor.RemoveProcess(commandName)
	po.RemoveProcess(commandName)
	// Wait for goroutine to print everything (watchdog closes stdin)
	exitCode := process.Cmd.ProcessState.ExitCode()
	po.consoleManager.MarkExited(prefixedProcessCommandName, exitCode)
	process.Cmd = nil
	return &exitCode, nil
}

func (pr *ProcessManager) WriteStdin(process *domain.Process, command string) error {

	if process.Cmd != nil {
		logger.Log().Info(command,
			zap.String("processName", process.Name),
		)

		if process.Type == "process_tty" {
			//write as raw as possible, no need to add newline or any fancy shit
			process.StdIn.Write([]byte(command))
		} else {
			io.WriteString(process.StdIn, command+"\n")
		}

		return nil
	}
	return errors.New("process not running")
}

func (pm *ProcessManager) GetRunningProcesses() map[string]*domain.Process {
	return pm.runningProcesses
}

func (pm *ProcessManager) AddRunningProcess(commandName string, process *domain.Process) {
	pm.runningProcesses[commandName] = process
}

func (pm *ProcessManager) GetRunningProcess(commandName string) *domain.Process {
	if process, ok := pm.GetRunningProcesses()[commandName]; ok {
		return process
	}
	return nil
}

func (pm *ProcessManager) RemoveProcess(commandName string) {
	delete(pm.runningProcesses, commandName)
}
