package services

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type ProcessManager struct {
	mu               sync.Mutex
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

	var exitCode int

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	combinedChannel := make(chan string, 20)
	go func() {
		defer close(combinedChannel)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				tmpBuffer := make([]byte, 1024)
				n, err := out.Read(tmpBuffer)
				if err != nil {
					return
				}
				combinedChannel <- string(tmpBuffer[:n])
			}
		}
	}()

	console, doneChan := po.consoleManager.AddConsoleWithChannel(commandName, domain.ConsoleTypeTTY, "stdin", combinedChannel)

	//reset periodically
	process.Cmd.Wait()
	cancel() // Signal the goroutine to stop

	po.processMonitor.RemoveProcess(commandName)
	po.RemoveProcess(commandName)
	// Wait for goroutine to print everything (watchdog closes stdin)
	exitCode = process.Cmd.ProcessState.ExitCode()
	console.MarkExited(exitCode)

	<-doneChan

	process.Cmd = nil

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

	//process.Cmd.SysProcAttr = &syscall.SysProcAttr{
	//	Setpgid: true,
	//}

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

	combinedChannel := make(chan string, 20)

	var wg sync.WaitGroup

	wg.Add(1)
	//read stdout
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutReader)
		for scanner.Scan() {
			text := scanner.Text()
			logger.Log().Debug(text)
			println(text)
			combinedChannel <- text + "\n"
		}
	}()

	wg.Add(1)
	//read stderr
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrReader)
		for scanner.Scan() {
			text := scanner.Text()
			logger.Log().Debug(text)
			println(text)
			combinedChannel <- text + "\n"
		}

	}()

	console, doneChan := po.consoleManager.AddConsoleWithChannel(commandName, domain.ConsoleTypeProcess, "stdin", combinedChannel)

	// Run and wait for Cmd to return, discard Status
	err = process.Cmd.Start()

	if err != nil {
		println("Error starting process", err)
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
		wg.Wait()

		err := process.Cmd.Wait()
		if err != nil {
			logger.Log().Error("Error waiting for process", zap.Error(err))
		}
		cmdDone()

		//stderrReader.Close()
		//stdoutReader.Close()
		//stdin.Close()
	}()

	<-cmdCtx.Done()

	po.processMonitor.RemoveProcess(commandName)
	po.RemoveProcess(commandName)
	// Wait for goroutine to print everything (watchdog closes stdin)
	exitCode := process.Cmd.ProcessState.ExitCode()

	console.MarkExited(exitCode)

	close(combinedChannel)
	//we wait, sothat we are sure all data is written to the console
	<-doneChan

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
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.runningProcesses[commandName] = process
}

func (pm *ProcessManager) GetRunningProcess(commandName string) *domain.Process {
	if process, ok := pm.GetRunningProcesses()[commandName]; ok {
		return process
	}
	return nil
}

func (pm *ProcessManager) RemoveProcess(commandName string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	delete(pm.runningProcesses, commandName)
}
