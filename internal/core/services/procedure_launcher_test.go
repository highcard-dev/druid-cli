package services_test

import (
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/core/services"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

func TestProcedureLauncherPassesCommandContextToRuntimeBackend(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	scrollService := mock_ports.NewMockScrollServiceInterface(ctrl)
	runtimeBackend := mock_ports.NewMockRuntimeBackendInterface(ctrl)
	command := &domain.CommandInstructionSet{
		Run: domain.RunModePersistent,
		Procedures: []*domain.Procedure{{
			Image: "alpine:3.20",
		}},
	}
	file := &domain.File{Ports: []domain.Port{{Name: "http", Port: 80}}}

	scrollService.EXPECT().GetCommand("serve").Return(command, nil)
	scrollService.EXPECT().GetFile().Return(file)
	runtimeBackend.EXPECT().RunCommand(gomock.Any()).DoAndReturn(func(runtimeCommand ports.RuntimeCommand) (*int, error) {
		if runtimeCommand.Name != "serve" {
			t.Fatalf("Name = %s, want serve", runtimeCommand.Name)
		}
		if runtimeCommand.Command != command {
			t.Fatal("Command was not forwarded to runtime backend")
		}
		if runtimeCommand.DataRoot != "/runtime-data" {
			t.Fatalf("DataRoot = %s, want /runtime-data", runtimeCommand.DataRoot)
		}
		if len(runtimeCommand.GlobalPorts) != 1 || runtimeCommand.GlobalPorts[0].Name != "http" {
			t.Fatalf("GlobalPorts = %#v", runtimeCommand.GlobalPorts)
		}
		return nil, nil
	})

	launcher, err := services.NewProcedureLauncher(scrollService, runtimeBackend, "/runtime-data")
	if err != nil {
		t.Fatal(err)
	}
	if err := launcher.Run("serve"); err != nil {
		t.Fatal(err)
	}
}
