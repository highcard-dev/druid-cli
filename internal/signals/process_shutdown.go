package signals

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	processutil "github.com/shirou/gopsutil/process"
	"go.uber.org/zap"
)

func SetupSignals(queueManager ports.QueueManagerInterface, processManager ports.ProcessManagerInterface, app *fiber.App, waitSeconds int) {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		os.Interrupt,
	//	syscall.SIGCHLD,
	)

	go func() {
		var s os.Signal
		select {
		case s = <-sigc:
			//debug timeout for testing
			//case <-time.After(time.Duration(25) * time.Second):
			//	s = syscall.SIGTERM
		}

		logger.Log().Info("Received shudown signal", zap.String("signal", s.String()))

		GracefulShutdown(queueManager, processManager, app, waitSeconds)
	}()
}

func GracefulShutdown(queueManager ports.QueueManagerInterface, processManager ports.ProcessManagerInterface, app *fiber.App, waitSeconds int) {

	go func() {
		for {
			if len(processManager.GetRunningProcesses()) == 0 {
				logger.Log().Info("No running processes")
				logger.Log().Info("Quitting...")
				os.Exit(0)
				app.Shutdown()
				break
			}
			logger.Log().Info(fmt.Sprintf("Waiting for %d processes to stop...", len(processManager.GetRunningProcesses())))
			time.Sleep(time.Second)
		}
	}()

	logger.Log().Info("Stopping all processes by defined routines")
	go queueManager.AddShutdownItem("stop") //TODO use stop types instead of name

	logger.Log().Info(fmt.Sprintf("Waiting for %d seconds...", waitSeconds))
	<-time.After(time.Minute)

	logger.Log().Info("Still not done, ending processes with SIGTERM")
	for _, process := range processManager.GetRunningProcesses() {
		pgid, err := syscall.Getpgid(process.Status().Pid)
		if err == nil {
			syscall.Kill(-pgid, 15) // note the minus sign
		} else {
			//normal stop without pgid
			process.Stop()
		}

	}

	logger.Log().Info(fmt.Sprintf("Waiting for %d seconds...", waitSeconds))
	<-time.After(time.Minute)

	logger.Log().Info("Still not done, killing all processes with SIGKILL")
	for _, process := range processManager.GetRunningProcesses() {
		p, err := processutil.NewProcess(int32(process.Status().Pid))
		if err != nil {
			break
		}
		running, _ := p.IsRunning()
		if running {
			pgid, err := syscall.Getpgid(process.Status().Pid)
			if err == nil {
				syscall.Kill(-pgid, 9) // note the minus sign
			} else {
				//normal stop without pgid
				process.Stop()
			}
		}
	}
	app.Shutdown()
	os.Exit(0)
}
