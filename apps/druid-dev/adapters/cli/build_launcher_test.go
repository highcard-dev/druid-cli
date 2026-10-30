package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/devapi"
)

func TestBuildLauncherWaitsAndRereadsRotatedCredential(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required by the dev image")
	}
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			tokenFile := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(tokenFile, []byte("before"), 0600); err != nil {
				t.Fatal(err)
			}
			var waits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/deployment/api/v1/command" {
					if r.Header.Get("Authorization") != "Bearer before" {
						t.Error("incorrect admission credential")
					}
					var input map[string]string
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input["command"] != "build_ui" {
						t.Error("wrong command payload")
					}
					if err := os.WriteFile(tokenFile, []byte("after"), 0600); err != nil {
						t.Error(err)
					}
					w.Write([]byte(`{"accepted":true}`))
					return
				}
				if r.URL.Path != "/deployment/api/v1/commands/build_ui/wait" || r.Header.Get("Authorization") != "Bearer after" {
					t.Error("wait did not use rotated credential or configured endpoint")
				}
				state := "done"
				if waits.Add(1) == 1 {
					state = "running"
				} else if fail {
					state = "error"
				}
				json.NewEncoder(w).Encode(map[string]any{"status": state})
			}))
			defer server.Close()
			command := exec.Command("sh", "../../../../scripts/build-ui.sh", "build_ui")
			command.Env = append(os.Environ(), "DRUID_API_URL="+server.URL+"/deployment", "DRUID_TOKEN_FILE="+tokenFile)
			output, err := command.CombinedOutput()
			if (err != nil) != fail || waits.Load() != 2 {
				t.Fatalf("err=%v waits=%d output=%s", err, waits.Load(), output)
			}
			if strings.Contains(string(output), "before") || strings.Contains(string(output), "after") {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestWatcherRerunsFiniteBuildWithoutWatchingItsOwnArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "private"), 0755); err != nil {
		t.Fatal(err)
	}
	server := newDevServer(root, devAuth{}, "private/custom.wasm", "private/layout.openui")
	defer server.stopWatch()
	err := server.startWatch(devapi.WatchModeRequest{WatchPaths: []string{"private"}, WorkingDirectory: ".", Command: []string{"sh", "-ec", "printf x >> count; printf wasm > private/custom.wasm; printf ui > private/layout.openui"}})
	if err != nil {
		t.Fatal(err)
	}
	waitCount := func(want int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			data, _ := os.ReadFile(filepath.Join(root, "count"))
			if len(data) == want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("did not reach %d builds", want)
	}
	waitCount(1)
	time.Sleep(700 * time.Millisecond)
	data, _ := os.ReadFile(filepath.Join(root, "count"))
	if len(data) != 1 {
		t.Fatal("artifacts triggered another build")
	}
	if err := os.WriteFile(filepath.Join(root, "private", "source.rs"), []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	waitCount(2)
	if string(server.watchResponse().Status) != statusReady {
		t.Fatal("finite build disabled watcher")
	}
}
