package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/highcard-dev/daemon/cmd"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/handler"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	test_utils "github.com/highcard-dev/daemon/test/utils"
	"github.com/otiai10/copy"
	"gopkg.in/yaml.v2"
)

func connectWs(addr string, console string) (*websocket.Conn, error) {

	u := url.URL{Scheme: "ws", Host: addr, Path: console}
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	return c, err
}
func waitForWs(host string, path string, duration time.Duration) (*websocket.Conn, error) {

	//connect to ws server and check logs
	var wsClient *websocket.Conn
	var err error

	timeout := time.After(duration)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return nil, errors.New("timeout waiting for ws connection")
		case <-ticker.C:
			wsClient, err = connectWs(host, path)
			if err == nil {
				return wsClient, nil
			}
		}
	}
}

func fetch(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	return buf.String(), nil
}

func waitForWsMessage(wsClient *websocket.Conn, message string, timeout time.Duration) error {
	for {
		select {
		case <-time.After(timeout):
			return fmt.Errorf("timeout waiting for message: %s", message)
		default:
			_, readMsg, err := wsClient.ReadMessage()
			if err != nil {
				return err
			}
			//print(string(readMsg))
			if strings.Contains(string(readMsg), message) {
				return nil
			}
		}
	}
}

func waitForConsoleRunning(console string, duration time.Duration) error {

	timeout := time.After(duration)

	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-timeout:
			return errors.New("timeout waiting for console to start")
		case <-ticker.C:
			body, err := fetch("http://localhost:8081/api/v1/consoles")
			if err != nil {
				continue
			}

			var resp handler.ConsolesHttpResponse

			json.Unmarshal([]byte(body), &resp)

			consoles := resp.Consoles

			if _, ok := consoles[console]; ok {
				return nil
			} else {
				keys := make([]string, 0, len(consoles))
				for k := range consoles {
					keys = append(keys, k)
				}
				log.Printf("console %s not found, found: %v", console, keys)
			}
		}
	}
}

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
			Name:       "TestServeFull With 5 Restarts",
			ScrollFile: "../../../examples/minecraft/.scroll/scroll.yaml",
			Restarts:   5,
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

				connected, err = startAndTestServeCommand(ctx, t, rootCmd)

				if !connected {
					t.Fatalf("Failed to connect to daemon web server: %v", err)
				}

				err = waitForConsoleRunning("start.0", 180*time.Second)
				if err != nil {
					t.Fatalf("Failed to start console: %v", err)
				}

				wsClient, err := waitForWs("localhost:8081", "/ws/v1/serve/start.0", 60*time.Second)
				if err != nil {
					t.Fatalf("Failed to connect to ws server: %v", err)
				}

				err = waitForWsMessage(wsClient, `For help, type "help"`, 60*time.Second)
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

				err = checkHttpServerShutdown(8081, 120*time.Second)
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
