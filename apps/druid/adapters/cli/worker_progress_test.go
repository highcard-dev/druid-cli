package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

func TestWorkerProgressReporterReadsSnapshotProgress(t *testing.T) {
	reports := make(chan int64, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var report struct {
			Token      string `json:"token"`
			Percentage int64  `json:"percentage"`
		}
		if err := json.NewDecoder(request.Body).Decode(&report); err != nil {
			t.Error(err)
		}
		if report.Token != "token" {
			t.Errorf("token = %q; want token", report.Token)
		}
		reports <- report.Percentage
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	progress := domain.NewSnapshotProgress()
	progress.Percentage.Store(37)
	stop := startWorkerProgressReporter(
		ports.RuntimeWorkerAction{
			RuntimeID:     "runtime-1",
			CallbackURL:   server.URL + "/internal/v1/workers/runtime-1/complete",
			CallbackToken: "token",
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
