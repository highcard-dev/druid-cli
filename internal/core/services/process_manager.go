package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"

	"github.com/highcard-dev/daemon/internal/core/domain"
	logger "github.com/highcard-dev/daemon/internal/core/services/log"
	"go.uber.org/zap"
)

type ProcessManager struct {
	runningProcesses map[string]*domain.Process
	logManager       *LogManager
	hub              *WebsocketBroadcaster
}

func NewProcessManager(logManager *LogManager, hub *WebsocketBroadcaster) *ProcessManager {
	return &ProcessManager{
		runningProcesses: make(map[string]*domain.Process),
		logManager:       logManager,
		hub:              hub,
	}
}

func (po *ProcessManager) Launch(process *domain.Process, command []string, dir string) error {

	if process.Cmd != nil {
		return errors.New("process already running")
	}

	cmdCtx, cmdDone := context.WithCancel(context.Background())

	//Split command to slice
	name, args := command[0], command[1:]

	logger.Log().Debug("Launch",
		zap.String("processName", name),
		zap.Strings("args", args),
		zap.String("dir", dir),
	)

	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	process.Cmd = exec.Command(name, args...)
	process.Cmd.Dir = dir
	process.Cmd.Stdout = stdoutWriter
	process.Cmd.Stderr = stderrWriter

	process.StdOut = stdoutReader
	process.StdErr = stderrReader
	stdin, err := process.Cmd.StdinPipe()

	if err != nil {
		cmdDone()
		return err
	}

	process.StdIn = stdin

	//process.Cmd.Start()

	go po.handleStdLine(process)

	//process.Cmd.Wait()

	// Run and wait for Cmd to return, discard Status
	err = process.Cmd.Start()

	if err != nil {
		cmdDone()
		process.Cmd = nil
		return err
	}

	go func() {
		_ = process.Cmd.Wait()
		cmdDone()
		stderrWriter.Close()
		stdoutWriter.Close()
		stdin.Close()
	}()

	<-cmdCtx.Done()

	// Wait for goroutine to print everything (watchdog closes stdin)
	process.Cmd = nil
	return nil
}

func (pm *ProcessManager) handleStdLine(process *domain.Process) {

	processStreamCommand := func(cmd domain.StreamCommand) {
		pm.logManager.AddLine("process", cmd)
		encoded, _ := json.Marshal(cmd)
		pm.hub.broadcast <- encoded
	}

	// Done when both channels have been closed
	//for process.Cmd != nil {

	go func() {
		scanner := bufio.NewScanner(process.StdOut)
		for scanner.Scan() {
			line := scanner.Text()

			logger.Log().LogStdout(process.Name, process.Name, line)

			d := domain.ProcessStreamCommand{
				SteamType: "stdout",
				Data:      line,
			}
			encoded, _ := json.Marshal(d)
			command := domain.StreamCommand{
				Stream: "process",
				Data:   string(encoded),
			}
			processStreamCommand(command)

		}
	}()

	go func() {
		scanner := bufio.NewScanner(process.StdErr)
		for scanner.Scan() {
			line := scanner.Text()

			logger.Log().LogStdout(process.Name, process.Name, line)

			d := domain.ProcessStreamCommand{
				SteamType: "stderr",
				Data:      line,
			}
			encoded, _ := json.Marshal(d)
			command := domain.StreamCommand{
				Stream: "process",
				Data:   string(encoded),
			}
			processStreamCommand(command)
		}
	}()
	/*
		select {
		case line, open := <-process.Cmd.StdoutPipe():
			if !open {
				continue
			}

			logger.Log().LogStdout(process.Name, process.Name, line)

			d := domain.ProcessStreamCommand{
				SteamType: "stdout",
				Data:      line,
			}
			encoded, _ := json.Marshal(d)
			command := domain.StreamCommand{
				Stream: "process",
				Data:   string(encoded),
			}
			processStreamCommand(command)
		case line, open := <-process.Cmd.Stderr:
			if !open {
				continue
			}

			logger.Log().LogStdout(process.Name, process.Name, line)
			d := domain.ProcessStreamCommand{
				SteamType: "stderr",
				Data:      line,
			}
			encoded, _ := json.Marshal(d)
			command := domain.StreamCommand{
				Stream: "process",
				Data:   string(encoded),
			}
			processStreamCommand(command)
		}*/
	//}
}

func (pr *ProcessManager) WriteStdin(process *domain.Process, command string) error {
	if process.Cmd != nil {
		logger.Log().Info(command,
			zap.String("processName", process.Name),
		)
		io.WriteString(process.StdIn, command+"\n")
		return nil
	}
	return errors.New("process not running")
}

func (pm *ProcessManager) GetRunningProcesses() map[string]*domain.Process {
	return pm.runningProcesses
}
