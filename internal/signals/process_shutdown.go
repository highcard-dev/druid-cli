package signals

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
	logger "github.com/highcard-dev/daemon/internal/core/services/log"
	log_drivers "github.com/highcard-dev/daemon/internal/core/services/log/drivers"
	processutil "github.com/shirou/gopsutil/process"
	"go.uber.org/zap"
)

func SetupSignals(scrollService ports.ScrollServiceInterface, app *fiber.App, waitSeconds int) {
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
		case s = <-log_drivers.Exit:
		case s = <-sigc:
		}

		logger.Log().Info("Received shudown signal", zap.String("signal", s.String()))

		GracefulShutdown(scrollService, app, waitSeconds)
	}()
}

func GracefulShutdown(scrollService ports.ScrollServiceInterface, app *fiber.App, waitSeconds int) {

	go func() {
		for {
			if len(scrollService.GetRunningProcesses()) == 0 {
				logger.Log().Info("No running processes")
				logger.Log().Info("Quitting...")
				os.Exit(0)
				app.Shutdown()
				break
			}
			time.Sleep(time.Second)
		}
	}()

	logger.Log().Info("Stopping all processes by defined routines")
	for processName := range scrollService.GetRunningProcesses() {
		go scrollService.Run("stop", processName, false) //TODO use stop types instead of name
	}

	logger.Log().Info(fmt.Sprintf("Waiting for %d seconds...", waitSeconds))
	<-time.After(time.Minute)

	logger.Log().Info("Still not done, ending processes with SIGTERM")
	for _, process := range scrollService.GetRunningProcesses() {
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
	for _, process := range scrollService.GetRunningProcesses() {
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
