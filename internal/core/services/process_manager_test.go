package services_test

import (
	"testing"

	"github.com/highcard-dev/daemon/internal/core/services"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

func TestProcessManager(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	logManager := mock_ports.NewMockLogManagerInterface(ctrl)
	consoleManager := services.NewConsoleManager(logManager)
	processMonitor := mock_ports.NewMockProcessMonitorInterface(ctrl)
	processManager := services.NewProcessManager(logManager, consoleManager, processMonitor)
	t.Run("Run", func(t *testing.T) {

		processMonitor.EXPECT().AddProcess(gomock.Any(), "echo.1").Times(1)
		processMonitor.EXPECT().RemoveProcess("echo.1").Times(1)
		logManager.EXPECT().AddLine("echo.1", []byte("hello\n")).Times(1)
		exitCode, err := processManager.Run("echo.1", []string{"echo", "hello"}, "/tmp")

		if err != nil {
			t.Error(err)
		}

		if *exitCode != 0 {
			t.Errorf("expected 0, got %d", exitCode)
		}
	})
	t.Run("RunTty", func(t *testing.T) {
		processMonitor.EXPECT().AddProcess(gomock.Any(), "echo.1").Times(1)
		processMonitor.EXPECT().RemoveProcess("echo.1").Times(1)

		logManager.EXPECT().AddLine("echo.1", gomock.Any()).MinTimes(1)
		exitCode, err := processManager.RunTty("echo.1", []string{"echo", "hello"}, "/tmp")

		if err != nil {
			t.Error(err)
		}

		if *exitCode != 0 {
			t.Errorf("expected 0, got %d", exitCode)
		}
	})
}
