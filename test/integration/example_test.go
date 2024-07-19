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

type ServiceConfig struct {
	ServiceName string
	ExamplePath string
	TestAddress string
	TestName    string
}

func TestExamples(t *testing.T) {

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configs := []ServiceConfig{
		{
			ServiceName: "minecraft",
			ExamplePath: "../../examples/minecraft/.scroll/scroll.yaml",
			TestAddress: "localhost:25565",
			TestName:    "Minecraft",
		},
		{
			ServiceName: "nginx",
			ExamplePath: "../../examples/nginx/.scroll/scroll.yaml",
			TestAddress: "localhost:80",
			TestName:    "Nginx",
		},
		// Add more services here
	}
	for _, config := range configs {
		t.Run(config.TestName, func(t *testing.T) {

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
			err = copy.Copy(config.ExamplePath, scrollPath+"scroll.yaml")
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

			go queueManager.Work()

			scrollService.WriteNewScrollLock()
			scrollService.Bootstrap(false)

			doneConnecting := make(chan error)
			doneStarting := make(chan error)

			if config.TestAddress != "" {
				// try to connect to TestAddress to see if the server is up, if yes end the test
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
							//TODO: UDP support, when we need it
							conn, err := net.DialTimeout("tcp", config.TestAddress, 1*time.Second)
							if err == nil {
								println("Connected to server after", time.Since(now).String())
								conn.Close()
								err = queueManager.AddItem("stop", false)
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
			}

			go func() {
				timeout := time.After(4 * time.Minute)
				tick := time.Tick(1 * time.Second)

				err := queueManager.AddItem("start", false)
				if err != nil {
					doneStarting <- err
					return
				}

				for {
					select {
					case <-timeout:
						t.Error("Timeout")
						doneStarting <- errors.New("Timeout Starting")
						return
					case <-tick:

						// If we are not testing a server, we can end the test here
						if config.TestAddress == "" {
							doneConnecting <- err
							return
						}
						if err != nil {
							t.Error(err)
							doneStarting <- err
							return
						}
					}
				}
			}()

			err = <-doneConnecting
			if err != nil {
				t.Error("Failed to test to server: ", err)
			}
		})
	}
}
