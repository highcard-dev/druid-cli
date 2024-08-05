package command_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/signals"
)

func fetchPorts() ([]domain.AugmentedPort, error) {
	body, err := fetch("http://localhost:8081/api/v1/ports")
	if err != nil {
		return nil, err
	}
	var ap []domain.AugmentedPort
	json.Unmarshal([]byte(body), &ap)
	return ap, nil
}

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
						Port:     12349,
						Name:     "testport",
						Protocol: "tcp",
					},
				},
				Init:     "start",
				Commands: testCommandTCP(),
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			println(tc.Name)

			_, path := setupScroll(t, tc.Scroll)
			defer os.RemoveAll(path)

			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(errors.New("test ended"))

			setupServeCmd(ctx, t, path, []string{"--watch-ports"})

			defer func() {
				signals.Stop()
			}()

			//give time to make sure everything is online
			time.Sleep(1 * time.Second)
			ap1, err := fetchPorts()
			if err != nil {
				t.Fatalf("Failed to fetch ports: %v", err)
			}

			for _, p := range ap1 {
				if !p.Open {
					t.Fatalf("Port %d is not open", p.Port.Port)
				}
			}
			//give time to to get picked up by the watcher
			time.Sleep(1 * time.Second)
			err = tcpTester("", 12349)

			//give time to to get picked up by the watcher
			time.Sleep(1 * time.Second)

			if err != nil {
				t.Fatalf("Failed to test tcp: %v", err)
			}

			ap2, err := fetchPorts()
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
