//go:build integration

package command_test

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/cmd"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	test_utils "github.com/highcard-dev/daemon/test/utils"
	"github.com/otiai10/copy"
	"gopkg.in/yaml.v2"
)

func TestServeCommand(t *testing.T) {

	type TestCase struct {
		Name             string
		ScrollFile       string
		Restarts         int
		RunModeOverwrite domain.RunMode
	}
	var testCases = []TestCase{
		{
			Name:       "TestServeFull",
			ScrollFile: "../../../examples/minecraft/.scroll/scroll.yaml",
			Restarts:   0,
		},
		{
			Name:       "TestServeFull With Restart",
			ScrollFile: "../../../examples/minecraft/.scroll/scroll.yaml",
			Restarts:   1,
		},
		{
			Name:       "TestServeFull With 3 Restarts",
			ScrollFile: "../../../examples/minecraft/.scroll/scroll.yaml",
			Restarts:   3,
		},
		{
			Name:             "TestServeFull With Restart (Persistent)",
			ScrollFile:       "../../../examples/minecraft/.scroll/scroll.yaml",
			Restarts:         1,
			RunModeOverwrite: domain.RunModePersistent,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			logger.Log(logger.WithStructuredLogging())

			time.Sleep(10 * time.Second)

			//observer := logger.SetupLogsCapture()
			unixTime := time.Now().Unix()
			path := "./druid-cli-test/" + strconv.FormatInt(unixTime, 10) + "/"
			scrollPath := path + ".scroll/"

			err := copy.Copy(tc.ScrollFile, scrollPath+"scroll.yaml")
			if err != nil {
				t.Fatalf("Failed to copy test scroll file: %v", err)
			}

			if tc.RunModeOverwrite != "" {
				//overwrite "restart" with RunModeOverwrite
				scroll, err := domain.NewScroll(scrollPath)
				if err != nil {
					t.Fatalf("Failed to read scroll file: %v", err)
				}
				for i, command := range scroll.File.Commands {
					if command.Run == domain.RunModeRestart {
						scroll.File.Commands[i].Run = domain.RunMode(tc.RunModeOverwrite)
					}
				}
				scrollBytes, err := yaml.Marshal(scroll.File)
				if err != nil {
					t.Fatalf("Failed to marshal scroll file: %v", err)
				}
				err = os.WriteFile(scrollPath+"scroll.yaml", scrollBytes, 0644)
				if err != nil {
					t.Fatalf("Failed to write scroll file: %v", err)
				}
			}

			if err := os.MkdirAll(path, 0755); err != nil {
				t.Fatalf("Failed to create test cwd: %v", err)
			}
			defer os.RemoveAll(path)

			runs := tc.Restarts + 1

			var installDate int64

			for i := 0; i < runs; i++ {
				var connected bool

				b := bytes.NewBufferString("")

				rootCmd := cmd.RootCmd
				rootCmd.SetErr(b)
				rootCmd.SetOut(b)
				rootCmd.SetArgs([]string{"--cwd", path, "serve", "--coldstarter=false"})

				ctx, cancel := context.WithCancel(context.WithValue(context.Background(), "disablePrometheus", true))

				defer cancel()

				logger.Log().Info("Starting serve command")

				connected, err = test_utils.StartAndTestServeCommand(ctx, t, rootCmd)

				if !connected {
					t.Fatalf("Failed to connect to daemon web server: %v", err)
				}

				err = test_utils.WaitForConsoleRunning("start.0", 180*time.Second)
				if err != nil {
					t.Fatalf("Failed to start console: %v", err)
				}

				wsClient, err := test_utils.WaitForWebsocketConnection("localhost:8081", "/ws/v1/serve/start-process", 60*time.Second)
				if err != nil {
					t.Fatalf("Failed to connect to ws server: %v", err)
				}

				err = test_utils.WaitForWebsocketMessage(wsClient, `For help, type "help"`, 60*time.Second)
				t.Log("Console message received")
				if err != nil {
					t.Fatalf("Failed to get help message: %v", err)
				}

				err = test_utils.ConnectionTest("localhost:25565", true)

				if err != nil {
					t.Fatalf("Failed to connect to minecraft server: %v", err)
				}

				t.Log("Connected to minecraft server")

				//double check that install was never run again
				lock, err := domain.ReadLock(scrollPath + "scroll-lock.json")
				if err != nil {
					t.Fatalf("Failed to read lock file: %v", err)
				}
				t.Log("Read lock file")

				if installDate == 0 {
					installDate = lock.GetStatus("install").LastStatusChange

					if installDate == 0 {
						t.Fatalf("Failed to get install date")
					}
				} else {
					if installDate != lock.GetStatus("install").LastStatusChange {
						t.Fatalf("Install command was run again")
					}
				}

				go func() {
					<-ctx.Done()
				}()

				t.Log("Stopping daemon server")

				cancel()

				err = test_utils.CheckHttpServerShutdown(8081, 120*time.Second)
				if err != nil {
					t.Fatalf("Failed to stop daemon server, server still online")
				}

				lock, err = domain.ReadLock(scrollPath + "scroll-lock.json")
				if err != nil {
					t.Fatalf("Failed to read lock file: %v", err)
				}

				expectedStatuses := map[string]domain.ScrollLockStatus{
					"install": "done",
					"start":   "waiting",
				}
				if tc.RunModeOverwrite == domain.RunModePersistent {
					expectedStatuses["start"] = "done"
				}

				for command, status := range expectedStatuses {
					s := lock.GetStatus(command)
					if s.Status != status {
						t.Fatalf("Lock file status %s not found, expected: %v, got: %v", status, expectedStatuses, lock.Statuses)
					}
				}

				t.Log("Stopped daemon server, lock file status looks good")

			}
		})

	}
}
