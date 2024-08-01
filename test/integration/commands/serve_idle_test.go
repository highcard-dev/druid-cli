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
	"github.com/spf13/cobra"
)

func checkHttpServer(port int) error {
	c, err := net.Dial("tcp", "localhost:"+strconv.Itoa(port))
	if c != nil {
		defer c.Close()
	}
	return err
}

func startAndTestServeCommand(ctx context.Context, t *testing.T, serveCmd *cobra.Command) (bool, error) {

	connectedChan := make(chan struct{}, 1)
	executionDoneChan := make(chan error, 1)

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if checkHttpServer(8081) == nil {
					connectedChan <- struct{}{}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		err := serveCmd.ExecuteContext(ctx)
		executionDoneChan <- err
	}()

	select {
	case <-connectedChan:
		t.Logf("Connected to server")
		return true, nil
	case err := <-executionDoneChan:
		return false, err
	}
}

func TestServeIdleCommand(t *testing.T) {

	type TestCase struct {
		Name        string
		Args        []string
		ExpectedErr error
	}
	var testCases = []TestCase{
		{
			Name:        "TestServeNoArtifact",
			Args:        []string{"serve"},
			ExpectedErr: errors.New("no artifact provided"),
		},
		{
			Name:        "TestServeNoArtifactTag",
			Args:        []string{"serve", "invalidscrollwithouttag"},
			ExpectedErr: errors.New("tag must be set"),
		},
		{
			Name:        "TestServeNoValidArtifact",
			Args:        []string{"serve", "invalidscroll:withtag"},
			ExpectedErr: errors.New("invalid reference: missing registry or repository"),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			//return
			//observer := logger.SetupLogsCapture()

			unixTime := time.Now().Unix()
			path := "./druid-cli-test/" + strconv.FormatInt(unixTime, 10) + "/"

			if err := os.MkdirAll(path, 0755); err != nil {
				t.Fatalf("Failed to create test cwd: %v", err)
			}
			defer os.RemoveAll(path)

			b := bytes.NewBufferString("")

			serveCmd := cmd.RootCmd
			serveCmd.SetErr(b)
			serveCmd.SetOut(b)
			serveCmd.SetArgs(append([]string{"--cwd", path}, tc.Args...))

			var err error
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err = serveCmd.ExecuteContext(ctx)

			if err != nil {
				if tc.ExpectedErr == nil {
					t.Fatalf("Unexpected error: %v", err)
				} else {
					if err.Error() != tc.ExpectedErr.Error() {
						t.Fatalf("Expected error: %v, got: %v", tc.ExpectedErr, err)
					}
				}
			}

		})

	}
}
