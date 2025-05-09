package signals

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	processutil "github.com/shirou/gopsutil/process"
	"go.uber.org/zap"
)

type SignalHandler struct {
	SigC           chan os.Signal
	queueManager   ports.QueueManagerInterface
	processManager ports.ProcessManagerInterface
	app            *fiber.App
	waitSeconds    int
}

func NewSignalHandler(ctx context.Context, queueManager ports.QueueManagerInterface, processManager ports.ProcessManagerInterface, app *fiber.App, waitSeconds int) *SignalHandler {
	sh := &SignalHandler{
		SigC:           make(chan os.Signal, 1),
		queueManager:   queueManager,
		processManager: processManager,
		app:            app,
		waitSeconds:    waitSeconds,
	}

	sh.SetupSignals(ctx)

	return sh
}

func (sh *SignalHandler) SetApp(app *fiber.App) {
	sh.app = app
}

func (sh *SignalHandler) SetupSignals(ctx context.Context) {

	signal.Notify(sh.SigC,
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
		case s = <-sh.SigC:
			logger.Log().Info("Received shudown signal", zap.String("signal", s.String()))
		case <-ctx.Done():
			logger.Log().Info("Context done")
			//debug timeout for testing
			//case <-time.After(time.Duration(25) * time.Second):
			//	s = syscall.SIGTERM
			go sh.queueManager.AddShutdownItem("stop")
		}

		sh.GracefulShutdown()
	}()
}

func (sh *SignalHandler) ExtendedShutdownRoutine() {

	shudownDone := make(chan struct{})
	go func() {
		waitForProcessesToStop(sh.processManager)
		shudownDone <- struct{}{}
	}()

	//TODO: refactor this
	done := false
	go func() {
		//wait for some time to await the sigterm
		<-time.After(time.Duration(sh.waitSeconds) * time.Second)
		go sh.queueManager.AddShutdownItem("stop")
		<-time.After(time.Duration(sh.waitSeconds) * time.Second)
		if done {
			return
		}
		go shutdownRoutine(sh.processManager, syscall.SIGTERM)
		<-time.After(time.Duration(sh.waitSeconds) * time.Second)
		if done {
			return
		}
		go shutdownRoutine(sh.processManager, syscall.SIGKILL)
	}()

	<-shudownDone
	done = true
}

func (sh *SignalHandler) GracefulShutdown() {

	logger.Log().Info("Graceful shutdown started")

	logger.Log().Info("Shutdown Routine")
	sh.ExtendedShutdownRoutine()

	logger.Log().Info("Shutting down app")
	if sh.app != nil {
		sh.app.Shutdown()
	}

	logger.Log().Info("Shutdown done")

}

func waitForProcessesToStop(processManager ports.ProcessManagerInterface) {
	for {
		if len(processManager.GetRunningProcesses()) == 0 {
			logger.Log().Info("No running processes")
			break
		}
		runningPorcesses := processManager.GetRunningProcesses()
		keys := slices.Collect(maps.Keys(runningPorcesses))

		logger.Log().Info(fmt.Sprintf("Waiting for %d processes to stop...", len(runningPorcesses)), zap.Strings("processes", keys))
		time.Sleep(time.Second)
	}
}

func shutdownRoutine(processManager ports.ProcessManagerInterface, signal syscall.Signal) {

	logger.Log().Info("Still not done, killing all processes with signal", zap.String("signal", signal.String()))
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

func (sh *SignalHandler) Stop() {
	sh.GracefulShutdown()

}
