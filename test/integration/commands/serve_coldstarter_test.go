package command_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/cmd"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/signals"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"gopkg.in/yaml.v2"
)

func writeScroll(scroll domain.File, path string) error {

	b, err := yaml.Marshal(scroll)
	if err != nil {
		return err
	}

	return os.WriteFile(path, b, 0644)
}

func waitUntilFileExists(path string, duration time.Duration) error {

	timeout := time.After(duration)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return errors.New("timeout waiting for file to exist")
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				return nil
			}
		}
	}
}

var genericHandler = "generic"
var testHandler = "test.lua"

var luaHandlerContent = `
function handle(data)
	finish()
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

var tcpTester = func() error {
	//tcp connect to 12349 and send test data
	con, err := net.DialTCP("tcp", nil, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12349})
	if err != nil {
		return err
	}
	defer con.Close()

	_, err = con.Write([]byte("test"))
	return err
}

var udpTester = func() error {
	//udp connect to 12349 and send test data
	con, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12349})
	if err != nil {
		return err
	}
	defer con.Close()

	_, err = con.Write([]byte("test"))
	return err
}

func TestColdstarterServeCommand(t *testing.T) {

	type TestCase struct {
		Name              string
		Scroll            domain.File
		ExecColdStarterFn func() error
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
						Port:     12349,
						Name:     "testport",
						Protocol: "tcp",
					},
				},
				Init:     "start",
				Commands: testCommand,
			},
		},
		{
			Name: "TestServeColdstarterWithoutHandler2",
			Scroll: domain.File{
				Ports: []domain.Port{
					{
						Port:     12349,
						Name:     "testport",
						Protocol: "tcp",
					},
					{
						Port:     12350,
						Name:     "testport2",
						Protocol: "tcp",
					},
				},
				Init:     "start",
				Commands: testCommand,
			},
		},
		{
			Name: "TestServeColdstarterWithGenericTCPHandler",
			Scroll: domain.File{
				Ports: []domain.Port{
					{
						Port:         12349,
						Name:         "testport",
						Protocol:     "tcp",
						SleepHandler: &genericHandler,
					},
				},
				Init:     "start",
				Commands: testCommand,
			},
			ExecColdStarterFn: tcpTester,
		},
		{
			Name: "TestServeColdstarterWithTestLuaHandler",
			Scroll: domain.File{
				Ports: []domain.Port{
					{
						Port:         12349,
						Name:         "testport",
						Protocol:     "tcp",
						SleepHandler: &testHandler,
					},
				},
				Init:     "start",
				Commands: testCommand,
			},
			LuaHandlerContent: luaHandlerContent,
			ExecColdStarterFn: tcpTester,
		},
		{
			Name: "TestServeColdstarterWithGenericUDPHandler",
			Scroll: domain.File{
				Ports: []domain.Port{
					{
						Port:         12349,
						Name:         "testport",
						Protocol:     "udp",
						SleepHandler: &genericHandler,
					},
				},
				Init:     "start",
				Commands: testCommand,
			},
			ExecColdStarterFn: udpTester,
		}, {
			Name: "TestServeColdstarterWithTestLuaHandler",
			Scroll: domain.File{
				Ports: []domain.Port{
					{
						Port:         12349,
						Name:         "testport",
						Protocol:     "udp",
						SleepHandler: &testHandler,
					},
				},
				Init:     "start",
				Commands: testCommand,
			},
			LuaHandlerContent: luaHandlerContent,
			ExecColdStarterFn: udpTester,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			println(tc.Name)
			logger.Log(logger.WithStructuredLogging())

			//observer := logger.SetupLogsCapture()
			unixTime := time.Now().Unix()
			path := "./druid-cli-test/" + strconv.FormatInt(unixTime, 10) + "/"
			scrollPath := path + ".scroll/"

			scroll := tc.Scroll

			if err := os.MkdirAll(scrollPath, 0755); err != nil {
				t.Fatalf("Failed to create test cwd: %v", err)
			}

			err := writeScroll(scroll, scrollPath+"scroll.yaml")
			if err != nil {
				t.Fatalf("Failed to write test scroll file: %v", err)
			}

			if tc.LuaHandlerContent != "" {
				err := os.WriteFile(scrollPath+testHandler, []byte(tc.LuaHandlerContent), 0644)
				if err != nil {
					t.Fatalf("Failed to write test lua handler file: %v", err)
				}
			}

			defer os.RemoveAll(path)

			b := bytes.NewBufferString("")

			serveCmd := cmd.RootCmd
			serveCmd.SetErr(b)
			serveCmd.SetOut(b)
			serveCmd.SetArgs([]string{"--cwd", path, "serve"})

			// Create a new context for each test case
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(errors.New("test ended"))

			cmd.ServeCommand.SetContext(ctx)

			connected, err := startAndTestServeCommand(ctx, t, serveCmd)
			if !connected {
				t.Fatalf("Failed to connect to daemon web server: %v", err)
			}

			if tc.ExecColdStarterFn != nil {
				//wait for server to start, maybe we can do this better, but we cannot do a tcp dial or somthing like that
				time.Sleep(1 * time.Second)
				err := tc.ExecColdStarterFn()
				if err != nil {
					t.Fatalf("Failed to execute coldstarter function: %v", err)
				}
			}

			defer func() {
				signals.Stop()
			}()

			err = waitUntilFileExists(path+"test.txt", 15*time.Second)
			if err != nil {
				t.Fatalf("Failed to wait for test.txt to be created: %v", err)
			}
		})

	}
}
