//go:build integration

package command_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	test_utils "github.com/highcard-dev/daemon/test/utils"
)

var genericHandler = "generic"
var testHandler = "test.lua"

var luaHandlerContent = `
function handle(ctx, data)
	if data == "test" then
		sendData("testback")
		finish()
	end
end
`

var testCommand = map[string]*domain.CommandInstructionSet{
	"start": {
		Procedures: []*domain.Procedure{
			{
				Mode: "exec",
				Data: []string{"touch", "test.txt"},
			},
		},
	},
}

func TestColdstarterServeCommand(t *testing.T) {

	type TestCase struct {
		Name              string
		Scroll            domain.File
		ExecColdStarterFn func(string, int) error
		LuaHandlerContent string
	}
	var testCases = []TestCase{

		{
			Name: "TestServeColdstarterEmtpty",
			Scroll: domain.File{
				Ports:    []domain.Port{},
				Init:     "start",
				Commands: testCommand,
			},
		},
		{
			Name: "TestServeColdstarterWithoutHandler",
			Scroll: domain.File{
				Ports: []domain.Port{
					{
						Port:     12350,
						Name:     "testport",
						Protocol: "tcp",
					},
				},
				Init:     "start",
				Commands: testCommand,
			},
			ExecColdStarterFn: test_utils.TcpTester,
		},
		{
			Name: "TestServeColdstarterWithoutHandler2",
			Scroll: domain.File{
				Ports: []domain.Port{
					{
						Port:     12350,
						Name:     "testport",
						Protocol: "tcp",
					},
					{
						Port:     12351,
						Name:     "testport2",
						Protocol: "tcp",
					},
				},
				Init:     "start",
				Commands: testCommand,
			},
			ExecColdStarterFn: test_utils.TcpTester,
		}, {
			Name: "TestServeColdstarterWithGenericTCPHandler",
			Scroll: domain.File{
				Ports: []domain.Port{
					{
						Port:         12352,
						Name:         "testport",
						Protocol:     "tcp",
						SleepHandler: &genericHandler,
					},
				},
				Init:     "start",
				Commands: testCommand,
			},
			ExecColdStarterFn: test_utils.TcpTester,
		}, {
			Name: "TestServeColdstarterWithTestLuaTCPHandler",
			Scroll: domain.File{
				Ports: []domain.Port{
					{
						Port:         12353,
						Name:         "testport",
						Protocol:     "tcp",
						SleepHandler: &testHandler,
					},
				},
				Init:     "start",
				Commands: testCommand,
			},
			LuaHandlerContent: luaHandlerContent,
			ExecColdStarterFn: test_utils.TcpTester,
		},
		{
			Name: "TestServeColdstarterWithGenericUDPHandler",
			Scroll: domain.File{
				Ports: []domain.Port{
					{
						Port:         12354,
						Name:         "testport",
						Protocol:     "udp",
						SleepHandler: &genericHandler,
					},
				},
				Init:     "start",
				Commands: testCommand,
			},
			ExecColdStarterFn: test_utils.UdpTester,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			logger.Log(logger.WithStructuredLogging())
			println(tc.Name)
			scrollPath, path := test_utils.SetupScroll(t, tc.Scroll)
			defer os.RemoveAll(path)

			if tc.LuaHandlerContent != "" {
				err := os.WriteFile(scrollPath+testHandler, []byte(tc.LuaHandlerContent), 0644)
				if err != nil {
					t.Fatalf("Failed to write test lua handler file: %v", err)
				}
			}
			ctx, cancel := context.WithCancelCause(context.WithValue(context.Background(), "disablePrometheus", true))

			defer cancel(errors.New("test ended"))

			test_utils.SetupServeCmd(ctx, t, path, []string{"--coldstarter"})

			if tc.ExecColdStarterFn != nil {
				//wait for server to start, maybe we can do this better, but we cannot do a tcp dial or somthing like that
				time.Sleep(1 * time.Second)
				var err error
				if tc.LuaHandlerContent != "" {
					err = tc.ExecColdStarterFn("testback", tc.Scroll.Ports[0].Port)
				} else {
					err = tc.ExecColdStarterFn("", tc.Scroll.Ports[0].Port)
				}
				if err != nil {
					t.Fatalf("Failed to execute coldstarter function: %v", err)
				}
			}

			err := test_utils.WaitUntilFileExists(path+"test.txt", 15*time.Second)
			if err != nil {
				t.Fatalf("Failed to wait for test.txt to be created: %v", err)
			}
		})

	}
}
