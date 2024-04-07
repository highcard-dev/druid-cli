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

		processMonitor.EXPECT().AddProcess(gomock.Any(), "test.echo").Times(1)
		processMonitor.EXPECT().RemoveProcess("test.echo").Times(1)
		logManager.EXPECT().AddLine("process.test.echo", []byte("hello")).Times(1)
		exitCode, err := processManager.Run("test", "echo", []string{"echo", "hello"}, "/tmp")

		if err != nil {
			t.Error(err)
		}

		if *exitCode != 0 {
			t.Errorf("expected 0, got %d", exitCode)
		}
	})
	t.Run("RunTty", func(t *testing.T) {
		processMonitor.EXPECT().AddProcess(gomock.Any(), "test.echo").Times(1)
		processMonitor.EXPECT().RemoveProcess("test.echo").Times(1)

		logManager.EXPECT().AddLine("process_tty.test.echo", gomock.Any()).MinTimes(1)
		exitCode, err := processManager.RunTty("test", "echo", []string{"echo", "hello"}, "/tmp")

		if err != nil {
			t.Error(err)
		}

		if *exitCode != 0 {
			t.Errorf("expected 0, got %d", exitCode)
		}
	})
}
