//go:build integration

package command_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	test_utils "github.com/highcard-dev/daemon/test/utils"
)

var testCommandTCP = func() map[string]*domain.CommandInstructionSet {
	var ncCommand = []string{"nc", "-l", "-p", "12349"}
	if runtime.GOOS == "darwin" {
		ncCommand = []string{"nc", "-l", "12349"}
	}
	return map[string]*domain.CommandInstructionSet{
		"start": {
			Procedures: []*domain.Procedure{
				{
					Mode: "exec",
					Data: ncCommand,
				},
			},
		},
	}
}

func TestWatchPortsServeCommand(t *testing.T) {

	type TestCase struct {
		Name   string
		Scroll domain.File
	}
	var testCases = []TestCase{
		{
			Name: "TestServeWaitPortsCommandTCP",
			Scroll: domain.File{
				Ports: []domain.Port{
					{
						Port:          12349,
						Name:          "testport",
						Protocol:      "tcp",
						CheckActivity: true,
					},
				},
				Init:     "start",
				Commands: testCommandTCP(),
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			logger.Log(logger.WithStructuredLogging())

			_, path := test_utils.SetupScroll(t, tc.Scroll)
			defer os.RemoveAll(path)

			ctx, cancel := context.WithCancelCause(context.WithValue(context.Background(), "disablePrometheus", true))
			defer cancel(errors.New("test ended"))

			test_utils.SetupServeCmd(ctx, t, path, []string{"--coldstarter=false", "--watch-ports"})
			//give time to make sure everything is online
			time.Sleep(1 * time.Second)
			ap1, err := test_utils.FetchPorts()
			if err != nil {
				t.Fatalf("Failed to fetch ports: %v", err)
			}

			fmt.Printf("Ports: %v\n", ap1)

			for _, p := range ap1 {
				if !p.Open {
					t.Fatalf("Port %d is not open", p.Port.Port)
				}
			}
			//give time to to get picked up by the watcher
			time.Sleep(1 * time.Second)

			err = test_utils.TcpTester("", 12349)
			if err != nil {
				t.Fatalf("Failed to test tcp: %v", err)
			}

			//give time to to get picked up by the watcher
			time.Sleep(1 * time.Second)

			ap2, err := test_utils.FetchPorts()
			if err != nil {
				t.Fatalf("Failed to fetch ports: %v", err)
			}

			for idx, p := range ap2 {
				if p.InactiveSince == ap1[idx].InactiveSince {
					t.Fatalf("InactiveSince did not change for port %d (both: %s)", p.Port.Port, p.InactiveSince)
				}
			}

		})
	}
}
