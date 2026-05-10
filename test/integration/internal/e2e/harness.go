package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type Binaries struct {
	Druid  string
	Client string
	Home   string
}

type Fixture struct {
	Dir           string
	Name          string
	ServeProc     string
	RecordProc    string
	Port          int
	RouteHost     string
	RouteURL      string
	RoutePort     int
	RoutingFile   string
	Assignment    string
	ScrollRootRef string
}

type RuntimeScroll struct {
	ID         string `json:"id"`
	DataRoot   string `json:"data_root"`
	ScrollName string `json:"scroll_name"`
	Status     string `json:"status"`
}

type RuntimePortStatus struct {
	Name      string `json:"name"`
	Procedure string `json:"procedure"`
	Port      int    `json:"port"`
	Bound     bool   `json:"bound"`
	HostPort  int    `json:"host_port"`
	Source    string `json:"source"`
}

type RuntimeRoutingTarget struct {
	Name        string `json:"name"`
	Procedure   string `json:"procedure"`
	PortName    string `json:"port_name"`
	Port        int    `json:"port"`
	Namespace   string `json:"namespace"`
	ServiceName string `json:"service_name"`
	ServicePort int    `json:"service_port"`
}

type LockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *LockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *LockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve repo root")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
}

func BuildBinaries(t *testing.T) Binaries {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	bins := Binaries{
		Druid:  filepath.Join(binDir, "druid"),
		Client: filepath.Join(binDir, "druid-client"),
		Home:   home,
	}
	build(t, "./apps/druid", bins.Druid)
	build(t, "./apps/druid-client", bins.Client)
	return bins
}

func build(t *testing.T, pkg string, output string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", output, pkg)
	cmd.Dir = RepoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build %s failed: %v\n%s", pkg, err, out)
	}
}

func StartDaemon(t *testing.T, bins Binaries, runtimeName string, socket string, stateDir string, extraArgs []string, extraEnv []string) *LockedBuffer {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(socket), 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{"serve", "--runtime", runtimeName, "--socket", socket, "--state-dir", stateDir}
	args = append(args, extraArgs...)
	cmd := exec.CommandContext(ctx, bins.Druid, args...)
	cmd.Dir = RepoRoot(t)
	cmd.Env = append(os.Environ(), append([]string{"HOME=" + bins.Home}, extraEnv...)...)
	logs := &LockedBuffer{}
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start druid daemon: %v\n%s", err, logs.String())
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return logs
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			t.Fatalf("druid daemon exited before socket became ready:\n%s", logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("druid daemon socket %s did not become ready:\n%s", socket, logs.String())
	return logs
}

func RunClient(t *testing.T, bins Binaries, socket string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	config := filepath.Join(bins.Home, "client.yaml")
	envFile := filepath.Join(bins.Home, ".env")
	fullArgs := append([]string{"--daemon-socket", socket, "--config", config, "--env-file", envFile}, args...)
	cmd := exec.CommandContext(ctx, bins.Client, fullArgs...)
	cmd.Dir = RepoRoot(t)
	cmd.Env = append(os.Environ(), "HOME="+bins.Home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("druid-client %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func RunClientJSON[T any](t *testing.T, bins Binaries, socket string, args ...string) T {
	t.Helper()
	out := RunClient(t, bins, socket, args...)
	var value T
	if err := json.Unmarshal([]byte(out), &value); err != nil {
		t.Fatalf("decode druid-client %s JSON: %v\n%s", strings.Join(args, " "), err, out)
	}
	return value
}

func FreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func WriteFixture(t *testing.T, dir string, name string, port int, routePort int) Fixture {
	t.Helper()
	suffix := strings.ToLower(strings.ReplaceAll(name, "_", "-"))
	serveProc := "web-" + suffix
	recordProc := "record-" + suffix
	routeHost := name + ".runtime.test"
	routeURL := fmt.Sprintf("http://%s:%d", routeHost, routePort)
	yaml := fmt.Sprintf(`name: %s
desc: CLI integration fixture with persistent data, a finite command, declared ports, and runtime env checks
version: 0.1.0
app_version: "test"
serve: serve
ports:
  - name: http
    protocol: http
    port: %d
    mandatory: true
commands:
  serve:
    run: persistent
    procedures:
      - id: %s
        image: busybox:1.36
        env:
          USER_ENV: fixture
          DRUID_PORT_HTTP: user-should-not-win
        expectedPorts:
          - name: http
            keepAliveTraffic: 1b/5m
        mounts:
          - path: /site
            sub_path: public
        command:
          - sh
          - -c
          - >-
            set -eu;
            mkdir -p /site;
            env | sort > /site/env.txt;
            printf 'healthy\n' > /site/index.txt;
            httpd -f -p %d -h /site
  record:
    run: once
    procedures:
      - id: %s
        image: busybox:1.36
        env:
          USER_ENV: finite
        mounts:
          - path: /data
        command:
          - sh
          - -c
          - >-
            set -eu;
            mkdir -p /data;
            printf 'finite-ok\n' > /data/finite.txt;
            env | sort > /data/record-env.txt
`, name, port, serveProc, port, recordProc)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scroll.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	routingFile := filepath.Join(dir, "routing.json")
	routing := fmt.Sprintf(`{
  "assignments": [
    {
      "name": "%s-http",
      "port_name": "http",
      "host": "%s",
      "external_ip": "127.0.0.1",
      "public_port": %d,
      "url": "%s",
      "protocol": "http"
    }
  ]
}`, serveProc, routeHost, routePort, routeURL)
	if err := os.WriteFile(routingFile, []byte(routing), 0644); err != nil {
		t.Fatal(err)
	}
	return Fixture{
		Dir:         dir,
		Name:        name,
		ServeProc:   serveProc,
		RecordProc:  recordProc,
		Port:        port,
		RouteHost:   routeHost,
		RouteURL:    routeURL,
		RoutePort:   routePort,
		RoutingFile: routingFile,
		Assignment:  serveProc + "-http",
	}
}

func WaitHTTP(t *testing.T, url string) string {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return string(body)
			}
			lastErr = fmt.Errorf("status %d body %q readErr %v", resp.StatusCode, string(body), readErr)
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("HTTP %s did not become ready: %v", url, lastErr)
	return ""
}

func ParseEnv(body string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}

func AssertRuntimeEnv(t *testing.T, env map[string]string, fixture Fixture, runtimeName string, scrollID string) {
	t.Helper()
	want := map[string]string{
		"DRUID_SCROLL_ID":          scrollID,
		"DRUID_SCROLL_NAME":        fixture.Name,
		"DRUID_RUNTIME_BACKEND":    runtimeName,
		"DRUID_PORT_HTTP":          fmt.Sprintf("%d", fixture.Port),
		"DRUID_PORT_HTTP_1":        fmt.Sprintf("%d", fixture.Port),
		"DRUID_PORT_HTTP_PROTOCOL": "http",
		"DRUID_PORT_HTTP_IP":       "127.0.0.1",
		"DRUID_IP":                 "127.0.0.1",
		"DRUID_PORT_HTTP_PUBLIC":   fmt.Sprintf("%d", fixture.RoutePort),
		"DRUID_PORT_HTTP_HOST":     fixture.RouteHost,
		"DRUID_PORT_HTTP_URL":      fixture.RouteURL,
	}
	for key, value := range want {
		if env[key] != value {
			t.Fatalf("%s = %q, want %q in env %#v", key, env[key], value, env)
		}
	}
	if env["DRUID_IP_WAIT"] != "" {
		t.Fatalf("DRUID_IP_WAIT = %q, want unset after routing assignment", env["DRUID_IP_WAIT"])
	}
	if env["DRUID_PORT_HTTP"] == "user-should-not-win" {
		t.Fatalf("runtime DRUID_PORT_HTTP did not override procedure env")
	}
}

func RequireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		if os.Getenv("CI") == "" {
			t.Skipf("%s is required for this integration test", name)
		}
		t.Fatalf("%s is required for this integration test: %v", name, err)
	}
}

func RequireDocker(t *testing.T) {
	t.Helper()
	RequireCommand(t, "docker")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "info")
	if out, err := cmd.CombinedOutput(); err != nil {
		if os.Getenv("CI") == "" {
			t.Skipf("Docker daemon is required for this integration test: %v\n%s", err, out)
		}
		t.Fatalf("Docker daemon is required for this integration test: %v\n%s", err, out)
	}
}

func Run(t *testing.T, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = RepoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}
