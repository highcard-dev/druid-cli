package test_utils

import (
	"errors"
	"net"
	"time"

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

func ConnectionTest(testAddress string, checkOnline bool) error {
	doneConnecting := make(chan error)

	// try to connect to TestAddress to see if the server is up, if yes end the test
	go func() {
		timeout := time.After(20 * time.Second)
		tick := time.Tick(1 * time.Second)
		now := time.Now()
		for {
			select {
			case <-timeout:
				doneConnecting <- errors.New("timeout Connecting")
				return
			case <-tick:
				conn, err := net.DialTimeout("tcp", testAddress, 1*time.Second)
				//TODO: UDP support, when we need it
				if err == nil {
					conn.Close()
					if checkOnline {
						println("Connected to server after", time.Since(now).String())
						doneConnecting <- nil
						return
					}
				} else {
					if !checkOnline {
						println("Server is offline after", time.Since(now).String())
						doneConnecting <- nil
						return
					}
				}
			}
		}
	}()

	return <-doneConnecting
}
