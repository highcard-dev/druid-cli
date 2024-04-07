package test_utils

import (
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	"go.uber.org/mock/gomock"
)

var processMonitor *mock_ports.MockProcessMonitorInterface

func GetMockedProcessMonitor(ctrl *gomock.Controller) *mock_ports.MockProcessMonitorInterface {
	if processMonitor == nil {
		processMonitor = mock_ports.NewMockProcessMonitorInterface(ctrl)
	}
	processMonitor.EXPECT().AddProcess(gomock.Any(), gomock.Any()).AnyTimes()
	processMonitor.EXPECT().RemoveProcess(gomock.Any()).AnyTimes()
	processMonitor.EXPECT().GetAllProcessesMetrics().AnyTimes()
	return processMonitor
}
