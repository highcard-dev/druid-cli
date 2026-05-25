package services_test

import (
	"errors"
	"fmt"
	"testing"

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

			scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
			runtimeBackend := mock_ports.NewMockRuntimeBackendInterface(ctrl)

			procedureLauncher, err := services.NewProcedureLauncher(scrollService, runtimeBackend, "/tmp")
			if err != nil {
				t.Error(err)
			}
			queueManager := services.NewQueueManager(scrollService, procedureLauncher)

			exitCode := 0
			runtimeBackend.EXPECT().Name().Return("docker").AnyTimes()
			runtimeBackend.EXPECT().RunCommand(gomock.Any()).Return(&exitCode, nil).Times(testCase.AccualExecution)

			scrollService.EXPECT().GetCommand("test").Return(&domain.CommandInstructionSet{
				Run: testCase.RunMode,
				Procedures: []*domain.Procedure{
					{
						Image:   "alpine:3.20",
						Command: []string{"echo", "hello"},
					},
				},
			}, nil).AnyTimes()

			scrollService.EXPECT().GetCwd().Return("/tmp").AnyTimes()
			scrollService.EXPECT().GetFile().Return(&domain.File{}).AnyTimes()

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

			scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)

			procedureLauncher := mock_ports.NewMockProcedureLauchnerInterface(ctrl)
			queueManager := services.NewQueueManager(scrollService, procedureLauncher)

			scrollService.EXPECT().GetCommand("test").Return(&domain.CommandInstructionSet{
				Run: testCase.RunMode,
				Procedures: []*domain.Procedure{
					{
						Image:   "alpine:3.20",
						Command: []string{"echo", "hello"},
					},
				},
			}, nil).AnyTimes()

			scrollService.EXPECT().GetCwd().Return("/tmp").AnyTimes()

			times := testCase.AccualExecution
			if testCase.RunMode == domain.RunModeOnce && testCase.Repeat > 1 {
				times = 2
			}

			first := true
			procedureLauncher.EXPECT().Run(gomock.Any()).DoAndReturn(func(cmd string) error {
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

	}

	t.Run("AddItem Deep Need Structure", func(t *testing.T) {

		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
		runtimeBackend := mock_ports.NewMockRuntimeBackendInterface(ctrl)

		procedureLauncher, err := services.NewProcedureLauncher(scrollService, runtimeBackend, "/tmp")
		if err != nil {
			t.Error(err)
		}
		queueManager := services.NewQueueManager(scrollService, procedureLauncher)

		exitCode := 0
		runtimeBackend.EXPECT().Name().Return("docker").AnyTimes()
		runtimeBackend.EXPECT().RunCommand(gomock.Any()).Return(&exitCode, nil).Times(4)

		scrollService.EXPECT().GetCommand("test").Return(&domain.CommandInstructionSet{
			Needs: []string{"dep1"},
			Procedures: []*domain.Procedure{
				{
					Image:   "alpine:3.20",
					Command: []string{"echo", "hello"},
				},
			},
		}, nil).AnyTimes()

		scrollService.EXPECT().GetCommand("dep1").Return(&domain.CommandInstructionSet{
			Needs: []string{"dep2.1", "dep2.2"},
			Procedures: []*domain.Procedure{
				{
					Image:   "alpine:3.20",
					Command: []string{"echo", "hello1"},
				},
			},
		}, nil).AnyTimes()
		scrollService.EXPECT().GetCommand("dep2.1").Return(&domain.CommandInstructionSet{
			Run: domain.RunModeOnce,
			Procedures: []*domain.Procedure{
				{
					Image:   "alpine:3.20",
					Command: []string{"echo", "hello2.1"},
				},
			},
		}, nil).AnyTimes()
		scrollService.EXPECT().GetCommand("dep2.2").Return(&domain.CommandInstructionSet{
			Procedures: []*domain.Procedure{
				{
					Image:   "alpine:3.20",
					Command: []string{"echo", "hello2.2"},
				},
			},
		}, nil).AnyTimes()

		scrollService.EXPECT().GetCwd().Return("/tmp").AnyTimes()
		scrollService.EXPECT().GetFile().Return(&domain.File{}).AnyTimes()

		go queueManager.Work()
		err = queueManager.AddTempItem("test")
		if err != nil {
			t.Error(err)
		}

		queueManager.WaitUntilEmpty()

		queue := queueManager.GetQueue()
		if queue["dep2.1"] != domain.ScrollLockStatusDone {
			t.Errorf("dep2.1 status must be done, got %s", queue["dep2.1"])
		}
	})
}

func TestQueueManagerRestartStopsOnNonRetryableError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	procedureLauncher := mock_ports.NewMockProcedureLauchnerInterface(ctrl)
	queueManager := services.NewQueueManager(scrollService, procedureLauncher)

	scrollService.EXPECT().GetCommand("start").Return(&domain.CommandInstructionSet{Run: domain.RunModeRestart}, nil).AnyTimes()
	procedureLauncher.EXPECT().Run("start").Return(domain.NonRetryableCommand(errors.New("port already in use"))).Times(1)

	go queueManager.Work()
	if err := queueManager.AddTempItem("start"); err != nil {
		t.Fatal(err)
	}
	queueManager.WaitUntilEmpty()

	if got := queueManager.GetQueue()["start"]; got != domain.ScrollLockStatusError {
		t.Fatalf("start = %s, want error", got)
	}
}

func TestQueueManagerRestartRetriesRetryableError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	procedureLauncher := mock_ports.NewMockProcedureLauchnerInterface(ctrl)
	queueManager := services.NewQueueManager(scrollService, procedureLauncher)

	scrollService.EXPECT().GetCommand("start").Return(&domain.CommandInstructionSet{Run: domain.RunModeRestart}, nil).AnyTimes()
	attempt := 0
	procedureLauncher.EXPECT().Run("start").DoAndReturn(func(string) error {
		attempt++
		if attempt == 1 {
			return errors.New("temporary crash")
		}
		return domain.NonRetryableCommand(errors.New("stop test"))
	}).Times(2)

	go queueManager.Work()
	if err := queueManager.AddTempItem("start"); err != nil {
		t.Fatal(err)
	}
	queueManager.WaitUntilEmpty()

	if attempt != 2 {
		t.Fatalf("attempts = %d, want retry", attempt)
	}
}

func TestQueueManagerStatusObserver(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	procedureLauncher := mock_ports.NewMockProcedureLauchnerInterface(ctrl)
	queueManager := services.NewQueueManager(scrollService, procedureLauncher)

	scrollService.EXPECT().GetCommand("test").Return(&domain.CommandInstructionSet{}, nil).AnyTimes()
	procedureLauncher.EXPECT().Run("test").Return(nil)

	observed := []domain.ScrollLockStatus{}
	queueManager.SetStatusObserver(func(command string, status domain.ScrollLockStatus, exitCode *int) {
		if command == "test" {
			observed = append(observed, status)
		}
	})

	go queueManager.Work()
	if err := queueManager.AddTempItem("test"); err != nil {
		t.Fatal(err)
	}
	queueManager.WaitUntilEmpty()

	want := []domain.ScrollLockStatus{
		domain.ScrollLockStatusWaiting,
		domain.ScrollLockStatusRunning,
		domain.ScrollLockStatusDone,
	}
	if len(observed) != len(want) {
		t.Fatalf("expected %d observed statuses, got %d: %v", len(want), len(observed), observed)
	}
	for i := range want {
		if observed[i] != want[i] {
			t.Fatalf("status %d = %s, want %s", i, observed[i], want[i])
		}
	}
}

func TestQueueManagerPersistentCommandCompletesWithoutLooping(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	procedureLauncher := mock_ports.NewMockProcedureLauchnerInterface(ctrl)
	queueManager := services.NewQueueManager(scrollService, procedureLauncher)

	scrollService.EXPECT().GetCommand("serve").Return(&domain.CommandInstructionSet{Run: domain.RunModePersistent}, nil).AnyTimes()
	procedureLauncher.EXPECT().Run("serve").Return(nil).Times(1)

	go queueManager.Work()
	if err := queueManager.AddTempItem("serve"); err != nil {
		t.Fatal(err)
	}
	queueManager.WaitUntilEmpty()

	if got := queueManager.GetQueue()["serve"]; got != domain.ScrollLockStatusDone {
		t.Fatalf("serve = %s, want done", got)
	}
}

func TestQueueManagerRememberDoneItemSatisfiesDependency(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	procedureLauncher := mock_ports.NewMockProcedureLauchnerInterface(ctrl)
	queueManager := services.NewQueueManager(scrollService, procedureLauncher)
	queueManager.RememberDoneItem("verify")

	scrollService.EXPECT().GetCommand("report").Return(&domain.CommandInstructionSet{Needs: []string{"verify"}}, nil).AnyTimes()
	scrollService.EXPECT().GetCommand("verify").Return(&domain.CommandInstructionSet{}, nil).AnyTimes()
	procedureLauncher.EXPECT().Run("report").Return(nil)

	go queueManager.Work()
	if err := queueManager.AddTempItem("report"); err != nil {
		t.Fatal(err)
	}
	queueManager.WaitUntilEmpty()

	queue := queueManager.GetQueue()
	if queue["report"] != domain.ScrollLockStatusDone {
		t.Fatalf("report = %s, want done; queue=%#v", queue["report"], queue)
	}
	if queue["verify"] != domain.ScrollLockStatusDone {
		t.Fatalf("verify = %s, want done; queue=%#v", queue["verify"], queue)
	}
}

func TestQueueManagerHydrateCommandStatuses(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	procedureLauncher := mock_ports.NewMockProcedureLauchnerInterface(ctrl)
	queueManager := services.NewQueueManager(scrollService, procedureLauncher)

	scrollService.EXPECT().GetCommand("install").Return(&domain.CommandInstructionSet{Run: domain.RunModeOnce}, nil).AnyTimes()
	scrollService.EXPECT().GetCommand("start").Return(&domain.CommandInstructionSet{Run: domain.RunModeRestart}, nil).AnyTimes()
	scrollService.EXPECT().GetCommand("serve").Return(&domain.CommandInstructionSet{Run: domain.RunModePersistent}, nil).AnyTimes()
	scrollService.EXPECT().GetCommand("repair").Return(&domain.CommandInstructionSet{}, nil).AnyTimes()

	if err := queueManager.HydrateCommandStatuses(map[string]domain.LockStatus{
		"install": {Status: domain.ScrollLockStatusDone},
		"start":   {Status: domain.ScrollLockStatusDone},
		"serve":   {Status: domain.ScrollLockStatusDone},
		"repair":  {Status: domain.ScrollLockStatusError},
	}); err != nil {
		t.Fatal(err)
	}

	queue := queueManager.GetQueue()
	if queue["install"] != domain.ScrollLockStatusDone {
		t.Fatalf("install = %s, want done", queue["install"])
	}
	if queue["start"] != domain.ScrollLockStatusWaiting {
		t.Fatalf("start = %s, want waiting", queue["start"])
	}
	if queue["serve"] != domain.ScrollLockStatusWaiting {
		t.Fatalf("serve = %s, want waiting", queue["serve"])
	}
	if queue["repair"] != domain.ScrollLockStatusWaiting {
		t.Fatalf("repair = %s, want waiting", queue["repair"])
	}
}

func TestQueueManagerAddForcedItemRerunsDoneOnceCommand(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	procedureLauncher := mock_ports.NewMockProcedureLauchnerInterface(ctrl)
	queueManager := services.NewQueueManager(scrollService, procedureLauncher)

	scrollService.EXPECT().GetCommand("start").Return(&domain.CommandInstructionSet{Run: domain.RunModeOnce}, nil).AnyTimes()
	procedureLauncher.EXPECT().Run("start").Return(nil).Times(2)

	go queueManager.Work()

	if err := queueManager.AddTempItem("start"); err != nil {
		t.Fatal(err)
	}
	queueManager.WaitUntilEmpty()
	if err := queueManager.AddTempItem("start"); err != services.ErrCommandDoneOnce {
		t.Fatalf("AddTempItem error = %v, want ErrCommandDoneOnce", err)
	}
	if err := queueManager.AddForcedItem("start"); err != nil {
		t.Fatal(err)
	}
	queueManager.WaitUntilEmpty()
}
