//go:build integration

package integration_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/services"
	mock_ports "github.com/highcard-dev/daemon/test/mock"
	test_utils "github.com/highcard-dev/daemon/test/utils"
	"github.com/otiai10/copy"
	"go.uber.org/mock/gomock"
)

type ServiceConfig struct {
	ServiceName    string
	ExamplePath    string
	TestAddress    string
	TestName       string
	LockFileStatus []string
	UseLogSpy      bool
	LogSpy         func(string, []byte) bool
}

func checkLockFile(scrollService *services.ScrollService, config ServiceConfig) error {

	lock, err := scrollService.GetLock()

	if err != nil {
		return err
	}

	for _, status := range config.LockFileStatus {
		if _, ok := lock.Statuses[status]; !ok {
			return fmt.Errorf("Lock file status %s not found, expected: %v, got: %v", status, config.LockFileStatus, lock.Statuses)
		}
	}
	return nil
}

func TestExamples(t *testing.T) {

	configs := []ServiceConfig{
		{
			ServiceName:    "minecraft",
			ExamplePath:    "../../examples/minecraft/.scroll/scroll.yaml",
			TestAddress:    "localhost:25565",
			TestName:       "Minecraft",
			LockFileStatus: []string{"start", "install"},
			UseLogSpy:      true,
			LogSpy: func(stream string, sc []byte) bool {
				println(string(sc))
				return strings.Contains(string(sc), `For help, type "help"`)
			},
		},
		{
			ServiceName:    "nginx",
			ExamplePath:    "../../examples/nginx/.scroll/scroll.yaml",
			TestAddress:    "localhost:80",
			TestName:       "Nginx",
			LockFileStatus: []string{"start"},
		},
		// Add more services here
	}
	for _, config := range configs {
		t.Run(config.TestName, func(t *testing.T) {

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			logManager := mock_ports.NewMockLogManagerInterface(ctrl)
			ociRegistryMock := mock_ports.NewMockOciRegistryInterface(ctrl)
			pluginManager := mock_ports.NewMockPluginManagerInterface(ctrl)

			pluginManager.EXPECT().HasMode(gomock.Any()).Return(false).AnyTimes()

			logDoneChan := make(chan struct{}, 1)

			logManager.EXPECT().AddLine(gomock.Any(), gomock.Any()).DoAndReturn(func(stream string, sc []byte) {
				if config.UseLogSpy {
					if config.LogSpy(stream, sc) {
						logDoneChan <- struct{}{}
					}
				}
			}).AnyTimes()

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
			scrollService.ReloadLock(false)

			err = queueManager.AddAndRememberItem("start")

			if err != nil {
				t.Error(err)
				return
			}

			if config.UseLogSpy {
				<-logDoneChan
			}

			if config.TestAddress != "" {
				err = test_utils.ConnectionTest(config.TestAddress, true)
				if err != nil {
					t.Error(err)
					return
				}
			}

			if err != nil {
				t.Error("Failed to test to server: ", err)
			}

			err = checkLockFile(scrollService, config)
			if err != nil {
				t.Error(err)
				return
			}

			err = queueManager.AddShutdownItem("stop")
			if err != nil {
				t.Error(err)
				return
			}

			if config.TestAddress != "" {
				err = test_utils.ConnectionTest(config.TestAddress, false)
				if err != nil {
					t.Error(err)
					return
				}
			}

			err = checkLockFile(scrollService, config)
			if err != nil {
				t.Error(err)
				return
			}

		})
	}
}
