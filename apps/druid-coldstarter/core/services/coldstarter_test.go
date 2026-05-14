package services

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/apps/druid-coldstarter/adapters/filesystem"
)

func TestColdstarterRunServesGenericPortAndWritesStatus(t *testing.T) {
	root := t.TempDir()
	port := freeTCPPort(t)
	scroll := []byte(`name: test/coldstarter
version: 0.1.0
ports:
  - name: main
    protocol: tcp
    port: ` + port + `
    sleep_handler: generic
commands: {}
`)
	if err := os.WriteFile(filepath.Join(root, "scroll.yaml"), scroll, 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- NewColdstarterService(filesystem.NewStatusWriter()).Run(ctx, root, ".coldstarter.json")
	}()

	conn := dialTCP(t, "127.0.0.1:"+port)
	_, _ = conn.Write([]byte("wake"))
	_ = conn.Close()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("coldstarter did not finish")
	}
	if _, err := os.Stat(filepath.Join(root, ".coldstarter.json")); err != nil {
		t.Fatalf("status file missing: %v", err)
	}
}

func freeTCPPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func dialTCP(t *testing.T, addr string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial %s: %v", addr, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
