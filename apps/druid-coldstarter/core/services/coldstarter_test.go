package services

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestColdstarterRunServesGenericPortFromEnv(t *testing.T) {
	root := t.TempDir()
	port := freeTCPPort(t)
	t.Setenv("DRUID_PORT_MAIN", port)
	t.Setenv("DRUID_PORT_MAIN_PROTOCOL", "tcp")
	t.Setenv("DRUID_PORT_MAIN_COLDSTARTER", "generic")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- NewColdstarterService().Run(ctx, root)
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
	if _, err := os.Stat(filepath.Join(root, ".coldstarter.json")); !os.IsNotExist(err) {
		t.Fatalf("status file exists or stat failed: %v", err)
	}
}

func TestColdstarterRunExitsFromSecondaryGenericPort(t *testing.T) {
	root := t.TempDir()
	mainPort := freeTCPPort(t)
	rconPort := freeTCPPort(t)
	t.Setenv("DRUID_PORT_MAIN", mainPort)
	t.Setenv("DRUID_PORT_MAIN_PROTOCOL", "tcp")
	t.Setenv("DRUID_PORT_MAIN_COLDSTARTER", "generic")
	t.Setenv("DRUID_PORT_RCON", rconPort)
	t.Setenv("DRUID_PORT_RCON_PROTOCOL", "tcp")
	t.Setenv("DRUID_PORT_RCON_COLDSTARTER", "generic")

	errCh := make(chan error, 1)
	go func() {
		errCh <- NewColdstarterService().Run(context.Background(), root)
	}()

	conn := dialTCP(t, "127.0.0.1:"+rconPort)
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
}

func TestColdstarterRejectsMissingPortEnv(t *testing.T) {
	t.Setenv("DRUID_PORT_MAIN_COLDSTARTER", "generic")

	err := NewColdstarterService().Run(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "DRUID_PORT_MAIN is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestColdstarterRejectsPathTraversalHandler(t *testing.T) {
	t.Setenv("DRUID_PORT_MAIN", freeTCPPort(t))
	t.Setenv("DRUID_PORT_MAIN_COLDSTARTER", "../minecraft.lua")

	err := NewColdstarterService().Run(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "path below DRUID_ROOT") {
		t.Fatalf("err = %v", err)
	}
}

func TestColdstarterRequiresConfiguredPorts(t *testing.T) {
	err := NewColdstarterService().Run(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no coldstarter ports configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestColdstarterAcceptsRelativeLuaHandler(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packet_handler"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRUID_PORT_MAIN", freeTCPPort(t))
	t.Setenv("DRUID_PORT_MAIN_COLDSTARTER", "packet_handler/minecraft.lua")
	t.Setenv("DRUID_COLDSTARTER_VAR_SERVER_LIST_NAME", "Druid idle")

	service, err := portServiceFromEnv(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := service.GetPorts()[0].ColdstarterHandler; got != "packet_handler/minecraft.lua" {
		t.Fatalf("handler = %q", got)
	}
	if got := service.GetPorts()[0].ColdstarterVars["SERVER_LIST_NAME"]; got != "Druid idle" {
		t.Fatalf("lua var = %q", got)
	}
}

func TestColdstarterRejectsMixedCaseEnvNames(t *testing.T) {
	t.Setenv("DRUID_PORT_MAIN", freeTCPPort(t))
	t.Setenv("DRUID_PORT_MAIN_COLDSTARTER", "generic")
	t.Setenv("DRUID_COLDSTARTER_VAR_"+"ServerListName", "Druid idle")

	err := NewColdstarterService().Run(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "must be uppercase") {
		t.Fatalf("err = %v", err)
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
