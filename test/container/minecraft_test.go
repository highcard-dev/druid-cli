package container_test

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/services"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	test_utils "github.com/highcard-dev/daemon/test/utils"
	"github.com/otiai10/copy"
	"go.uber.org/mock/gomock"
)

func TestContainerMinecraft(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logManager := mock_ports.NewMockLogManagerInterface(ctrl)
	ociRegistryMock := mock_ports.NewMockOciRegistryInterface(ctrl)
	pluginManager := mock_ports.NewMockPluginManagerInterface(ctrl)

	pluginManager.EXPECT().HasMode(gomock.Any()).Return(false).AnyTimes()

	logManager.EXPECT().AddLine(gomock.Any(), gomock.Any()).MinTimes(2)

	unixTime := time.Now().Unix()

	path := "./druid-cli-test/"

	//ensure the path exists and is empty
	os.RemoveAll(path)
	path = path + strconv.FormatInt(unixTime, 10) + "/"
	scrollPath := path + ".scroll/"
	err := os.MkdirAll(path, 0755)

	if err != nil {
		t.Error(err)
		return
	}

	//copy example
	err = copy.Copy("../../examples/minecraft/.scroll/scroll.yaml", scrollPath+"scroll.yaml")
	if err != nil {
		t.Error(err)
		return
	}

	scrollService, err := services.NewScrollService(path)
	if err != nil {
		t.Error(err)
		return
	}
	consoleManager := services.NewConsoleManager(logManager)
	processMonitor := test_utils.GetMockedProcessMonitor(ctrl)
	processManager := services.NewProcessManager(logManager, consoleManager, processMonitor)
	procedureLauncher := services.NewProcedureLauncher(ociRegistryMock, processManager, pluginManager, consoleManager, logManager, scrollService)

	queueManager := services.NewQueueManager(scrollService, procedureLauncher)

	t.Run("Launch real app from examples", func(t *testing.T) {

		scrollService.WriteNewScrollLock()
		scrollService.Bootstrap(false)

		doneConnecting := make(chan error)
		doneStarting := make(chan error)

		// try to connect to 25565 to see if the server is up, if yes end the test
		go func() {
			timeout := time.After(3 * time.Minute)
			tick := time.Tick(1 * time.Second)
			now := time.Now()
			for {
				select {
				case <-timeout:
					t.Error("Timeout")
					doneConnecting <- errors.New("Timeout Connecting")
					return
				case <-tick:
					conn, err := net.DialTimeout("tcp", "localhost:25565", 1*time.Second)
					if err == nil {
						println("Connected to server after", time.Since(now).String())
						conn.Close()
						err = queueManager.AddShutdownItem("stop")
						if err != nil {
							t.Error(err)
							doneConnecting <- err
							return
						}
						doneConnecting <- nil
						return
					}
				case e := <-doneStarting:
					doneConnecting <- fmt.Errorf("Failed to start server: %v", e)
				}
			}
		}()

		// try to connect to 25565 to see if the server is up, if yes end the test
		go func() {
			timeout := time.After(4 * time.Minute)
			tick := time.Tick(1 * time.Second)

			for {
				select {
				case <-timeout:
					t.Error("Timeout")
					doneStarting <- errors.New("Timeout Starting")
					return
				case <-tick:
					err := queueManager.AddTempItem("start")
					if err != nil {
						t.Error(err)
						doneStarting <- err
						return
					}
				}
			}
		}()

		err := <-doneConnecting
		if err != nil {
			t.Error("Failed to connect to server: ", err)
		}
	})
}
