package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

func TestWorkerProgressReporterReadsSnapshotProgress(t *testing.T) {
	reports := make(chan int64, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var report struct {
			Percentage int64 `json:"percentage"`
		}
		if err := json.NewDecoder(request.Body).Decode(&report); err != nil {
			t.Error(err)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer worker-token" {
			t.Errorf("authorization = %q; want Bearer worker-token", got)
		}
		reports <- report.Percentage
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	progress := domain.NewSnapshotProgress()
	progress.Percentage.Store(37)
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("worker-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stop := startWorkerProgressReporter(
		ports.RuntimeWorkerAction{
			RuntimeID:   "runtime-1",
			CallbackURL: server.URL + "/internal/v1/workers/runtime-1/complete",
			TokenFile:   tokenFile,
		},
		progress,
		time.Hour,
	)
	defer stop()

	select {
	case percentage := <-reports:
		if percentage != 37 {
			t.Fatalf("percentage = %d; want 37", percentage)
		}
	case <-time.After(time.Second):
		t.Fatal("progress was not reported")
	}
}
