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
	Druid string
	Home  string
}

type Fixture struct {
	Dir        string
	Name       string
	ServeProc  string
	RecordProc string
	Port       int
	RouteHost  string
	RouteURL   string
	RoutePort  int
}

type RuntimeScroll struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Root   string `json:"root"`
}

type RuntimePortStatus struct {
	Name      string `json:"name"`
	Procedure string `json:"procedure"`
	Port      int    `json:"port"`
	Bound     bool   `json:"bound"`
	HostPort  int    `json:"host_port"`
}

type RuntimeRoutingTarget struct {
	Procedure   string `json:"procedure"`
	PortName    string `json:"port_name"`
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
		Druid: filepath.Join(binDir, "druid"),
		Home:  home,
	}
	build(t, "./apps/druid", bins.Druid)
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
	args := []string{"daemon", "--runtime", runtimeName, "--socket", socket, "--state-dir", stateDir}
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
	cmd := exec.CommandContext(ctx, bins.Druid, fullArgs...)
	cmd.Dir = RepoRoot(t)
	cmd.Env = append(os.Environ(), "HOME="+bins.Home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("druid %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func RunClientJSON[T any](t *testing.T, bins Binaries, socket string, args ...string) T {
	t.Helper()
	out := RunClient(t, bins, socket, args...)
	var value T
	if err := json.Unmarshal([]byte(out), &value); err != nil {
		t.Fatalf("decode druid %s JSON: %v\n%s", strings.Join(args, " "), err, out)
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
	routeHost := "localhost"
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
	return Fixture{
		Dir:        dir,
		Name:       name,
		ServeProc:  serveProc,
		RecordProc: recordProc,
		Port:       port,
		RouteHost:  routeHost,
		RouteURL:   routeURL,
		RoutePort:  routePort,
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
	return RunEnv(t, nil, name, args...)
}

func RunEnv(t *testing.T, env []string, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = RepoRoot(t)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func BuildDockerImage(t *testing.T, tag string) string {
	t.Helper()
	contextDir := t.TempDir()
	druid := filepath.Join(contextDir, "druid")
	coldstarter := filepath.Join(contextDir, "druid-coldstarter")
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer buildCancel()
	buildCmd := exec.CommandContext(buildCtx, "go", "build", "-o", druid, "./apps/druid")
	buildCmd.Dir = RepoRoot(t)
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+runtime.GOARCH)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("linux druid build failed: %v\n%s", err, out)
	}
	buildCmd = exec.CommandContext(buildCtx, "go", "build", "-o", coldstarter, "./apps/druid-coldstarter")
	buildCmd.Dir = RepoRoot(t)
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+runtime.GOARCH)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("linux druid-coldstarter build failed: %v\n%s", err, out)
	}
	dockerfile := `FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY druid druid-coldstarter /usr/bin/
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
`
	if err := os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		t.Fatal(err)
	}
	entrypoint := `#!/bin/sh
if [ "$1" = "druid-coldstarter" ] || [ "$1" = "/usr/bin/druid-coldstarter" ]; then
	exec "$@"
fi
exec druid "$@"
`
	if err := os.WriteFile(filepath.Join(contextDir, "entrypoint.sh"), []byte(entrypoint), 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "build", contextDir, "-t", tag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker build failed: %v\n%s", err, out)
	}
	return tag
}

func StartRegistry(t *testing.T) int {
	t.Helper()
	port := FreePort(t)
	name := fmt.Sprintf("druid-e2e-registry-%d", time.Now().UnixNano())
	Run(t, "docker", "run", "-d", "--rm", "--name", name, "-p", fmt.Sprintf("127.0.0.1:%d:5000", port), "registry:2")
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", name).Run()
	})
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/v2/", port))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return port
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("registry did not become ready on port %d", port)
	return 0
}

func DockerHostAddress(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return "host.docker.internal"
	}
	gateway := strings.TrimSpace(Run(t, "docker", "network", "inspect", "bridge", "--format", "{{(index .IPAM.Config 0).Gateway}}"))
	if gateway == "" || gateway == "<no value>" {
		return "host.docker.internal"
	}
	return gateway
}

func UnixJSONRequest(t *testing.T, socket string, method string, path string, body string) string {
	t.Helper()
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, addr string) (net.Conn, error) {
			return net.Dial("unix", socket)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Minute}
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, "http://druid"+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode >= 400 {
		t.Fatalf("%s %s failed with %d: %s", method, path, resp.StatusCode, data)
	}
	return string(data)
}
