package services_test

import (
	"errors"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/services"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

func TestQueueManagerRunCommandDelegatesToLauncher(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	procedureLauncher := mock_ports.NewMockProcedureLauchnerInterface(ctrl)
	queueManager := services.NewQueueManager(scrollService, procedureLauncher)

	procedureLauncher.EXPECT().Run("start").Return(nil)

	if err := queueManager.RunCommand("start"); err != nil {
		t.Fatal(err)
	}
}

func TestDeriveCommandStatusFromProcedures(t *testing.T) {
	command := &domain.CommandInstructionSet{Procedures: []*domain.Procedure{{}, {}}}

	tests := []struct {
		name     string
		statuses map[string]domain.LockStatus
		want     domain.ScrollLockStatus
		wantOK   bool
	}{
		{
			name:   "missing",
			wantOK: false,
		},
		{
			name: "done",
			statuses: map[string]domain.LockStatus{
				"start.0": {Status: domain.ScrollLockStatusDone},
				"start.1": {Status: domain.ScrollLockStatusDone},
			},
			want:   domain.ScrollLockStatusDone,
			wantOK: true,
		},
		{
			name: "running",
			statuses: map[string]domain.LockStatus{
				"start.0": {Status: domain.ScrollLockStatusDone},
				"start.1": {Status: domain.ScrollLockStatusRunning},
			},
			want:   domain.ScrollLockStatusRunning,
			wantOK: true,
		},
		{
			name: "waiting",
			statuses: map[string]domain.LockStatus{
				"start.0": {Status: domain.ScrollLockStatusDone},
			},
			want:   domain.ScrollLockStatusWaiting,
			wantOK: true,
		},
		{
			name: "error",
			statuses: map[string]domain.LockStatus{
				"start.0": {Status: domain.ScrollLockStatusError},
			},
			want:   domain.ScrollLockStatusError,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := services.DeriveCommandStatusFromProcedures("start", command, tt.statuses)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("status = %s, ok = %v; want %s, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestCommandExitCode(t *testing.T) {
	exitCode := services.CommandExitCode(&domain.CommandExecutionError{ExitCode: 23, Err: errors.New("failed")})
	if exitCode == nil || *exitCode != 23 {
		t.Fatalf("exitCode = %v, want 23", exitCode)
	}
	if exitCode := services.CommandExitCode(errors.New("plain")); exitCode != nil {
		t.Fatalf("exitCode = %v, want nil", exitCode)
	}
}
