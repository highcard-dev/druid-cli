package command_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/cmd"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
)

func checkHttpServer(port int) error {
	c, err := net.Dial("tcp", "localhost:"+strconv.Itoa(port))
	if c != nil {
		defer c.Close()
	}
	return err
}

func startAndTestServeCommand(ctx context.Context, t *testing.T, rootCmd *cobra.Command) (bool, error) {

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

	go func(ctx context.Context) {
		cmd.ServeCommand.SetContext(ctx)

		err := rootCmd.ExecuteContext(ctx)

		if err != nil {
			executionDoneChan <- err
			return
		}

		fmt.Printf("Context at serve command %v\n", ctx)

		executionDoneChan <- nil
	}(ctx)

	select {
	case <-connectedChan:
		t.Logf("Connected to server")
		return true, nil
	case err := <-executionDoneChan:
		t.Logf("Execution done")
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

			logger.Log(logger.WithStructuredLogging())

			unixTime := time.Now().Unix()
			path := "./druid-cli-test/" + strconv.FormatInt(unixTime, 10) + "/"

			if err := os.MkdirAll(path, 0755); err != nil {
				t.Fatalf("Failed to create test cwd: %v", err)
			}
			defer os.RemoveAll(path)

			b := bytes.NewBufferString("")

			rootCmd := cmd.RootCmd
			rootCmd.SetErr(b)
			rootCmd.SetOut(b)
			rootCmd.SetArgs(append([]string{"--cwd", path}, tc.Args...))

			ctx := context.Background()

			serveCmd, _, err := rootCmd.Find([]string{"serve"})
			if err != nil {
				t.Fatalf("Failed to find serve command: %v", err)
			}
			serveCmd.SetContext(ctx)

			err = rootCmd.ExecuteContext(ctx)

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
