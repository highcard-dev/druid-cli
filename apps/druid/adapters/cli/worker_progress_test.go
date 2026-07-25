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
	reports := make(chan float64, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var report struct {
			Token      string  `json:"token"`
			Percentage float64 `json:"percentage"`
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
	progress.StorePercentage(37.4)
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
		if percentage != 37.4 {
			t.Fatalf("percentage = %v; want 37.4", percentage)
		}
	case <-time.After(time.Second):
		t.Fatal("progress was not reported")
	}
}
