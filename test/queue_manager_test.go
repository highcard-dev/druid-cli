package services_test

import (
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

type CommandTest struct {
	Repeat          int
	AccualExecution int
	RunMode         domain.RunMode
}

func TestQueueManager(t *testing.T) {

	testCases := []CommandTest{
		{
			Repeat:          1,
			AccualExecution: 1,
			RunMode:         domain.RunModeAlways,
		},
		{
			Repeat:          5,
			AccualExecution: 5,
			RunMode:         domain.RunModeAlways,
		},
		{
			Repeat:          1,
			AccualExecution: 1,
			RunMode:         domain.RunModeOnce,
		},
		{
			Repeat:          2,
			AccualExecution: 1,
			RunMode:         domain.RunModeOnce,
		},
		{
			Repeat:          5,
			AccualExecution: 1,
			RunMode:         domain.RunModeOnce,
		},
	}

	for _, testCase := range testCases {

		t.Run(fmt.Sprintf("AddItem (RunMode: %s, Repeat: %d)", testCase.RunMode, testCase.Repeat), func(t *testing.T) {
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

			processMonitor.EXPECT().AddProcess(gomock.Any(), "test.0").AnyTimes()
			processMonitor.EXPECT().RemoveProcess("test.0").AnyTimes()

			scrollService.EXPECT().GetCommand("test").Return(&domain.CommandInstructionSet{
				Run: testCase.RunMode,
				Procedures: []*domain.Procedure{
					{
						Mode: "exec",
						Wait: nil,
						Data: []interface{}{"echo", "hello"},
					},
				},
			}, nil).AnyTimes()

			pluginManager.EXPECT().HasMode(gomock.Any()).Return(false).AnyTimes()

			logManager.EXPECT().AddLine("test.0", []byte("hello\n")).Times(testCase.AccualExecution)

			scrollService.EXPECT().GetLock().Return(&domain.ScrollLock{
				Statuses:      map[string]domain.ScrollLockStatus{},
				ScrollVersion: semver.MustParse("1.0.0"),
				ScrollName:    "test",
			}, nil).AnyTimes()

			scrollService.EXPECT().GetCwd().Return("/tmp").AnyTimes()

			go queueManager.Work()

			for i := 0; i < testCase.Repeat; i++ {
				err := queueManager.AddTempItem("test")
				if err != nil {
					if testCase.RunMode == domain.RunModeOnce && err == services.ErrCommandDoneOnce {
						continue
					}
					t.Error(err)
				}
				queueManager.WaitUntilEmpty()
			}
		})

		t.Run(fmt.Sprintf("AddItem error first, but after that succeeds (RunMode: %s, Repeat: %d)", testCase.RunMode, testCase.Repeat), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			processMonitor := mock_ports.NewMockProcessMonitorInterface(ctrl)
			scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)

			procedureLauncher := mock_ports.NewMockProcedureLauchnerInterface(ctrl)
			queueManager := services.NewQueueManager(scrollService, procedureLauncher)

			processMonitor.EXPECT().AddProcess(gomock.Any(), "test").AnyTimes()
			processMonitor.EXPECT().RemoveProcess("test").AnyTimes()

			scrollService.EXPECT().GetCommand("test").Return(&domain.CommandInstructionSet{
				Run: testCase.RunMode,
				Procedures: []*domain.Procedure{
					{
						Mode: "exec",
						Wait: nil,
						Data: []interface{}{"echo", "hello"},
					},
				},
			}, nil).AnyTimes()

			scrollService.EXPECT().GetLock().Return(&domain.ScrollLock{
				Statuses:      map[string]domain.ScrollLockStatus{},
				ScrollVersion: semver.MustParse("1.0.0"),
				ScrollName:    "test",
			}, nil).AnyTimes()

			scrollService.EXPECT().GetCwd().Return("/tmp").AnyTimes()

			times := testCase.AccualExecution
			if testCase.RunMode == domain.RunModeOnce && testCase.Repeat > 1 {
				times = 2
			}

			first := true
			procedureLauncher.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(cmd string, runCommandCb func(cmd string) error) error {
				if first {
					first = false
					return fmt.Errorf("error")
				} else {
					return nil
				}
			}).Times(times)

			go queueManager.Work()

			for i := 0; i < testCase.Repeat; i++ {
				err := queueManager.AddTempItem("test")

				if err != nil {
					if testCase.RunMode == domain.RunModeOnce && err == services.ErrCommandDoneOnce {
						continue
					}
					t.Error(err)
				}
				queueManager.WaitUntilEmpty()
			}
		})

		t.Run(fmt.Sprintf("AddItem Command  (RunMode: %s, Repeat: %d)", testCase.RunMode, testCase.Repeat), func(t *testing.T) {
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

			processMonitor.EXPECT().AddProcess(gomock.Any(), "test.0").AnyTimes()
			processMonitor.EXPECT().RemoveProcess("test.0").AnyTimes()

			scrollService.EXPECT().GetCommand("test").Return(&domain.CommandInstructionSet{
				Run: testCase.RunMode,
				Procedures: []*domain.Procedure{
					{
						Mode: "exec",
						Wait: nil,
						Data: []interface{}{"echo", "hello"},
					},
				},
			}, nil).AnyTimes()

			scrollService.EXPECT().GetCommand("test_command").Return(&domain.CommandInstructionSet{
				Procedures: []*domain.Procedure{
					{
						Mode: "command",
						Wait: nil,
						Data: "test",
					},
				},
			}, nil).AnyTimes()

			pluginManager.EXPECT().HasMode(gomock.Any()).Return(false).AnyTimes()

			logManager.EXPECT().AddLine("test.0", []byte("hello\n")).Times(testCase.AccualExecution)

			scrollService.EXPECT().GetLock().Return(&domain.ScrollLock{
				Statuses:      map[string]domain.ScrollLockStatus{},
				ScrollVersion: semver.MustParse("1.0.0"),
				ScrollName:    "test",
			}, nil).AnyTimes()

			scrollService.EXPECT().GetCwd().Return("/tmp").AnyTimes()

			go queueManager.Work()

			for i := 0; i < testCase.Repeat; i++ {
				err := queueManager.AddTempItem("test_command")
				if err != nil {
					t.Error(err)
				}

				queueManager.WaitUntilEmpty()
			}
		})
	}

	t.Run("AddItem Deep Need Structure", func(t *testing.T) {

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

		lock := &domain.ScrollLock{
			Statuses: map[string]domain.ScrollLockStatus{},
		}
		scrollService.EXPECT().GetLock().Return(lock, nil).AnyTimes()
		processMonitor.EXPECT().AddProcess(gomock.Any(), gomock.Any()).Times(4)
		//processMonitor.EXPECT().AddProcess(gomock.Any(), "dep1").Times(1)
		//processMonitor.EXPECT().AddProcess(gomock.Any(), "test").Times(1)

		processMonitor.EXPECT().RemoveProcess(gomock.Any()).Times(4)
		//processMonitor.EXPECT().RemoveProcess("dep1").Times(1)
		//processMonitor.EXPECT().RemoveProcess("test").Times(1)

		scrollService.EXPECT().GetCommand("test").Return(&domain.CommandInstructionSet{
			Needs: []string{"dep1"},
			Procedures: []*domain.Procedure{
				{
					Mode: "exec",
					Wait: nil,
					Data: []interface{}{"echo", "hello"},
				},
			},
		}, nil).AnyTimes()

		scrollService.EXPECT().GetCommand("dep1").Return(&domain.CommandInstructionSet{
			Needs: []string{"dep2.1", "dep2.2"},
			Procedures: []*domain.Procedure{
				{
					Mode: "exec",
					Wait: nil,
					Data: []interface{}{"echo", "hello1"},
				},
			},
		}, nil).AnyTimes()
		scrollService.EXPECT().GetCommand("dep2.1").Return(&domain.CommandInstructionSet{
			Run: domain.RunModeOnce,
			Procedures: []*domain.Procedure{
				{
					Mode: "exec",
					Wait: nil,
					Data: []interface{}{"echo", "hello2.1"},
				},
			},
		}, nil).AnyTimes()
		scrollService.EXPECT().GetCommand("dep2.2").Return(&domain.CommandInstructionSet{
			Procedures: []*domain.Procedure{
				{
					Mode: "exec",
					Wait: nil,
					Data: []interface{}{"echo", "hello2.2"},
				},
			},
		}, nil).AnyTimes()

		pluginManager.EXPECT().HasMode(gomock.Any()).Return(false).AnyTimes()

		logManager.EXPECT().AddLine(gomock.Any(), gomock.Any()).Times(4)
		//logManager.EXPECT().AddLine("process.dep1", gomock.Eq([]byte("hello1\n"))).Times(1)
		//logManager.EXPECT().AddLine("test.0", gomock.Eq([]byte("hello\n"))).Times(1)

		scrollService.EXPECT().GetLock().Return(&domain.ScrollLock{
			Statuses:      map[string]domain.ScrollLockStatus{},
			ScrollVersion: semver.MustParse("1.0.0"),
			ScrollName:    "test",
		}, nil).AnyTimes()

		scrollService.EXPECT().GetCwd().Return("/tmp").AnyTimes()

		go queueManager.Work()
		err := queueManager.AddTempItem("test")
		if err != nil {
			t.Error(err)
		}

		queueManager.WaitUntilEmpty()

		if len(lock.Statuses) != 1 {
			t.Errorf("Lock status must be 1 (dep2.1) but got %d", len(lock.Statuses))
		}
	})
}
