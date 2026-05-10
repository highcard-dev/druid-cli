//go:build integration && legacy_examples

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
	ServiceName   string
	ExamplePath   string
	TestAddress   string
	TestName      string
	CommandStatus []string
	UseLogSpy     bool
	LogSpy        func(string, []byte) bool
}

func checkQueue(queueManager *services.QueueManager, config ServiceConfig) error {
	queue := queueManager.GetQueue()
	for _, status := range config.CommandStatus {
		if _, ok := queue[status]; !ok {
			return fmt.Errorf("command status %s not found, expected: %v, got: %v", status, config.CommandStatus, queue)
		}
	}
	return nil
}

func TestExamples(t *testing.T) {

	configs := []ServiceConfig{
		{
			ServiceName:   "minecraft",
			ExamplePath:   "../../examples/minecraft/scroll.yaml",
			TestAddress:   "localhost:25565",
			TestName:      "Minecraft",
			CommandStatus: []string{"start", "install"},
			UseLogSpy:     true,
			LogSpy: func(stream string, sc []byte) bool {
				println(string(sc))
				return strings.Contains(string(sc), `For help, type "help"`)
			},
		},
		{
			ServiceName:   "nginx",
			ExamplePath:   "../../examples/nginx/scroll.yaml",
			TestAddress:   "localhost:80",
			TestName:      "Nginx",
			CommandStatus: []string{"start"},
		},
		// Add more services here
	}
	for _, config := range configs {
		t.Run(config.TestName, func(t *testing.T) {

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			logManager := mock_ports.NewMockLogManagerInterface(ctrl)
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
			err := os.MkdirAll(path, 0755)

			if err != nil {
				t.Error(err)
				return
			}

			//copy example scroll.yaml directly into the scroll directory
			err = copy.Copy(config.ExamplePath, path+"scroll.yaml")
			if err != nil {
				t.Error(err)
				return
			}

			scrollService, err := services.NewScrollService(path)
			if err != nil {
				t.Error(err)
				return
			}
			runtimeBackend := mock_ports.NewMockRuntimeBackendInterface(ctrl)
			exitCode := 0
			runtimeBackend.EXPECT().Name().Return("docker").AnyTimes()
			runtimeBackend.EXPECT().RunCommand(gomock.Any()).Return(&exitCode, nil).AnyTimes()
			procedureLauncher, err := services.NewProcedureLauncher(scrollService, runtimeBackend, "/tmp")
			if err != nil {
				t.Error(err)
				return
			}
			queueManager := services.NewQueueManager(scrollService, procedureLauncher)

			go queueManager.Work()

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

			err = checkQueue(queueManager, config)
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

			err = checkQueue(queueManager, config)
			if err != nil {
				t.Error(err)
				return
			}

		})
	}
}
