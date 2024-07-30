package container_test

import (
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

func TestContainerNginx(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logManager := mock_ports.NewMockLogManagerInterface(ctrl)
	ociRegistryMock := mock_ports.NewMockOciRegistryInterface(ctrl)
	pluginManager := mock_ports.NewMockPluginManagerInterface(ctrl)

	pluginManager.EXPECT().HasMode(gomock.Any()).Return(false).AnyTimes()

	logManager.EXPECT().AddLine("process.main.start", gomock.Any()).AnyTimes()

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
	err = copy.Copy("../../examples/nginx/.scroll/scroll.yaml", scrollPath+"scroll.yaml")
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

		err := queueManager.AddTempItem("start")
		if err != nil {
			t.Error(err)
		}
	})
}
