package services

import (
	"context"
	"errors"
	"io"
	"os/exec"

	"github.com/creack/pty"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/logger"
	"go.uber.org/zap"
)

type ProcessManager struct {
	runningProcesses map[string]*domain.Process
	logManager       *LogManager
	consoleManager   ports.ConsoleManagerInterface
	processManager   ports.ProcessMonitorInterface
}

func NewProcessManager(logManager *LogManager, consoleManager ports.ConsoleManagerInterface, processManager ports.ProcessMonitorInterface) *ProcessManager {
	return &ProcessManager{
		runningProcesses: make(map[string]*domain.Process),
		logManager:       logManager,
		consoleManager:   consoleManager,
		processManager:   processManager,
	}
}

func (po *ProcessManager) RunTty(processName string, command []string, cwd string) error {

	process := domain.Process{
		Name: processName,
		Type: "process_tty",
	}

	if process.Cmd != nil {
		return errors.New("process already running")
	}

	name, args := command[0], command[1:]

	logger.Log().Debug("LaunchTty",
		zap.String("processName", name),
		zap.Strings("args", args),
		zap.String("dir", cwd),
	)

	process.Cmd = exec.Command(name, args...)
	process.Cmd.Dir = cwd

	logger.Log().Info("Starting tty process", zap.String("processName", processName), zap.String("name", name), zap.Strings("args", args), zap.String("dir", cwd))

	out, err := pty.Start(process.Cmd)
	if err != nil {
		return err
	}

	process.StdIn = out

	//self register process
	po.AddRunningProcess(processName, &process)

	//add process for monitoring
	po.processManager.AddProcess(int32(process.Cmd.Process.Pid), process.Name)

	//slight difference to normal process, as we only attach after the process has started
	//add console output
	po.consoleManager.AddConsole("process_tty", "tty", "stdin", out)

	//reset periodically
	process.Cmd.Wait()
	po.processManager.RemoveProcess(processName)

	return nil
}

func (po *ProcessManager) Run(processName string, command []string, dir string) error {

	process := domain.Process{
		Name: processName,
		Type: "process",
	}
	//Todo, add processmonitoring explicitly here
	if process.Cmd != nil {
		return errors.New("process already running")
	}

	cmdCtx, cmdDone := context.WithCancel(context.Background())

	//Split command to slice
	name, args := command[0], command[1:]

	logger.Log().Debug("Launch",
		zap.String("processName", processName),
		zap.String("name", name),
		zap.Strings("args", args),
		zap.String("dir", dir),
	)

	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	process.Cmd = exec.Command(name, args...)
	process.Cmd.Dir = dir
	process.Cmd.Stdout = stdoutWriter
	process.Cmd.Stderr = stderrWriter

	stdin, err := process.Cmd.StdinPipe()

	if err != nil {
		cmdDone()
		return err
	}

	process.StdIn = stdin

	// Run and wait for Cmd to return, discard Status
	err = process.Cmd.Start()

	if err != nil {
		cmdDone()
		process.Cmd = nil
		return err
	}

	//self register process
	po.AddRunningProcess(processName, &process)

	//add process for monitoring
	po.processManager.AddProcess(int32(process.Cmd.Process.Pid), process.Name)

	//add console output

	//WARNING MultiReader is not working as expected, it seems to block the process and process.Wait() never returns
	//stdReader := io.MultiReader(stdoutReader, stderrReader)

	po.consoleManager.AddConsole("process", "process", "stdin", stdoutReader)
	po.consoleManager.AddConsole("process", "process", "stdin", stderrReader)

	go func() {
		_ = process.Cmd.Wait()
		cmdDone()
		//stderrWriter.Close() //TODO: THis most likely can be removed
		stdoutWriter.Close()
		stdin.Close()
	}()

	<-cmdCtx.Done()

	po.processManager.RemoveProcess(processName)
	// Wait for goroutine to print everything (watchdog closes stdin)
	process.Cmd = nil
	return nil
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

func (pm *ProcessManager) AddRunningProcess(name string, process *domain.Process) {
	pm.runningProcesses[name] = process
}

func (pm *ProcessManager) GetRunningProcess(name string) *domain.Process {
	if process, ok := pm.GetRunningProcesses()[name]; ok {
		return process
	}
	return nil
}
