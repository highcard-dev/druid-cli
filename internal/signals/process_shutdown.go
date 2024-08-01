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

var SigC chan os.Signal
var shutdownDone chan struct{}

func SetupSignals(queueManager ports.QueueManagerInterface, processManager ports.ProcessManagerInterface, app *fiber.App, waitSeconds int) {
	SigC = make(chan os.Signal, 1)
	shutdownDone = make(chan struct{})

	signal.Notify(SigC,
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
		case s = <-SigC:
			//debug timeout for testing
			//case <-time.After(time.Duration(25) * time.Second):
			//	s = syscall.SIGTERM
		}

		logger.Log().Info("Received shudown signal", zap.String("signal", s.String()))

		GracefulShutdown(queueManager, processManager, app, waitSeconds)
		shutdownDone <- struct{}{}
	}()
}

func GracefulShutdown(queueManager ports.QueueManagerInterface, processManager ports.ProcessManagerInterface, app *fiber.App, waitSeconds int) {

	shudownDone := make(chan struct{})
	go func() {
		waitForProcessesToStop(processManager)
		app.Shutdown()
		shudownDone <- struct{}{}
	}()

	//we do that non block, in case something is allready down
	go queueManager.AddShutdownItem("stop")

	go func() {
		<-time.After(time.Duration(waitSeconds) * time.Second)
		go shutdownRoutine(processManager, syscall.SIGTERM)
		<-time.After(time.Duration(waitSeconds) * time.Second)
		go shutdownRoutine(processManager, syscall.SIGKILL)
	}()

	<-shudownDone
	logger.Log().Info("Shutdown done")

}

func waitForProcessesToStop(processManager ports.ProcessManagerInterface) {
	for {
		if len(processManager.GetRunningProcesses()) == 0 {
			logger.Log().Info("No running processes")
			break
		}
		logger.Log().Info(fmt.Sprintf("Waiting for %d processes to stop...", len(processManager.GetRunningProcesses())))
		time.Sleep(time.Second)
	}
}

func shutdownRoutine(processManager ports.ProcessManagerInterface, signal syscall.Signal) {

	logger.Log().Info("Still not done, killing all processes with SIGKILL")
	for _, process := range processManager.GetRunningProcesses() {
		p, err := processutil.NewProcess(int32(process.Status().Pid))
		if err != nil {
			break
		}
		running, _ := p.IsRunning()
		if running {
			//pgid, err := syscall.Getpgid(process.Status().Pid)
			//if err == nil {
			//	syscall.Kill(-pgid, signal) // note the minus sign
			//} else {
			//normal stop without pgid
			process.Cmd.Process.Signal(signal)
			//}
		}
	}
}

func Stop() {
	SigC <- syscall.SIGTERM
	<-shutdownDone
}
