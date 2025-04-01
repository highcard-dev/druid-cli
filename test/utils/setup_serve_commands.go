package test_utils

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/cmd"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
)

func StartAndTestServeCommand(ctx context.Context, t *testing.T, rootCmd *cobra.Command) (bool, error) {

	connectedChan := make(chan struct{}, 1)
	executionDoneChan := make(chan error, 1)

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if CheckHttpServer(8081, time.Second*20) == nil {
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

func SetupServeCmd(ctx context.Context, t *testing.T, cwd string, additionalArgs []string) {

	args := append([]string{"--cwd", cwd, "serve"}, additionalArgs...)

	b := bytes.NewBufferString("")

	serveCmd := cmd.RootCmd
	serveCmd.SetErr(b)
	serveCmd.SetOut(b)
	serveCmd.SetArgs(args)
	// Create a new context for each test case

	cmd.ServeCommand.SetContext(ctx)

	logger.Log().Info(fmt.Sprintf("Running serve command with args: %v", args))

	connected, err := StartAndTestServeCommand(ctx, t, serveCmd)
	if !connected {
		t.Fatalf("Failed to connect to daemon web server: %v", err)
	}
}
