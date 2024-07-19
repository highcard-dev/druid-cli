package services_test

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

func TestProcedureLauncher(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logManager := mock_ports.NewMockLogManagerInterface(ctrl)
	processMonitor := mock_ports.NewMockProcessMonitorInterface(ctrl)
	ociRegistryMock := mock_ports.NewMockOciRegistryInterface(ctrl)
	pluginManager := mock_ports.NewMockPluginManagerInterface(ctrl)
	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)

	consoleManager := services.NewConsoleManager(logManager)
	processManager := services.NewProcessManager(logManager, consoleManager, processMonitor)
	procedureLauncher := services.NewProcedureLauncher(ociRegistryMock, processManager, pluginManager, consoleManager, logManager, scrollService)
	queueManager := services.NewQueueManager(scrollService, procedureLauncher)

	t.Run("RunNew", func(t *testing.T) {
		processMonitor.EXPECT().AddProcess(gomock.Any(), "test").Times(1)
		processMonitor.EXPECT().RemoveProcess("test").Times(1)

		scrollService.EXPECT().GetCommand("test").Return(&domain.CommandInstructionSet{
			Procedures: []*domain.Procedure{
				{
					Mode: "exec",
					Wait: nil,
					Data: []interface{}{"echo", "hello"},
				},
			},
		}, nil).AnyTimes()

		pluginManager.EXPECT().HasMode("exec").Return(false)

		logManager.EXPECT().AddLine("process.test", []byte("hello\n")).Times(1)

		scrollService.EXPECT().GetLock().Return(&domain.ScrollLock{
			Statuses:      map[string]domain.ScrollLockStatus{},
			ScrollVersion: semver.MustParse("1.0.0"),
			ScrollName:    "test",
		}, nil).AnyTimes()

		scrollService.EXPECT().GetCwd().Return("/tmp").AnyTimes()

		go queueManager.Work()
		err := queueManager.AddItem("test", false)
		if err != nil {
			t.Error(err)
		}

		queueManager.WaitUntilEmpty()
	})
}
