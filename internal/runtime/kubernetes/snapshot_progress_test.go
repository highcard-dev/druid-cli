package kubernetes

import (
	"strings"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func TestObserveSnapshotProgressReadsSteamCMDByteMarker(t *testing.T) {
	progress := domain.NewSnapshotProgress()
	line := `DRUID_PROGRESS_V1 {"step_id":"steamcmd","stage":"downloading","label":"Downloading server files","unit":"bytes","current":12549178426,"total":22938933947,"total_final":true}`

	if !observeSnapshotProgress(line, progress) {
		t.Fatal("progress marker was not recognized")
	}
	if got := progress.Percentage(); got != 54.7 {
		t.Fatalf("percentage = %v; want 54.7", got)
	}
}

func TestObserveSnapshotProgressReadsLowProgressByteMarker(t *testing.T) {
	progress := domain.NewSnapshotProgress()
	line := `DRUID_PROGRESS_V1 {"unit":"bytes","current":401991074,"total":22938933947}`

	if !observeSnapshotProgress(line, progress) {
		t.Fatal("progress marker was not recognized")
	}
	if got := progress.Percentage(); got != 1.8 {
		t.Fatalf("percentage = %v; want 1.8", got)
	}
}

func TestObserveSnapshotProgressReadsOriginalSteamCMDLine(t *testing.T) {
	progress := domain.NewSnapshotProgress()
	line := "\x1b[0m Update state (0x61) downloading, progress: 54.71 (12549178426 / 22938933947)"

	if observeSnapshotProgress(line, progress) {
		t.Fatal("original SteamCMD output should remain visible in the console")
	}
	if got := progress.Percentage(); got != 54.7 {
		t.Fatalf("percentage = %v; want 54.7", got)
	}
}

func TestReadSnapshotProgressDoesNotDependOnConsoleConsumer(t *testing.T) {
	output := make(chan string, 1)
	output <- "older console line"
	progress := domain.NewSnapshotProgress()
	done := make(chan struct{})

	go func() {
		_ = readSnapshotProgress(
			strings.NewReader(
				"ordinary console line\n"+
					`DRUID_PROGRESS_V1 {"unit":"bytes","current":12549178426,"total":22938933947}`+"\n",
			),
			progress,
		)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("progress processing blocked behind the console backlog")
	}
	if got := progress.Percentage(); got != 54.7 {
		t.Fatalf("percentage = %v; want 54.7", got)
	}
	if got := <-output; got != "older console line" {
		t.Fatalf("console backlog changed to %q", got)
	}
}

func TestObserveSnapshotProgressLeavesMalformedLinesAlone(t *testing.T) {
	progress := domain.NewSnapshotProgress()
	progress.StorePercentage(17)

	for _, line := range []string{
		"ordinary server output",
		`DRUID_PROGRESS_V1 {"unit":"bytes","current":10,"total":0}`,
		`DRUID_PROGRESS_V1 {"unit":"items","current":10,"total":20}`,
	} {
		if observeSnapshotProgress(line, progress) {
			t.Fatalf("line was unexpectedly recognized: %s", line)
		}
		if got := progress.Percentage(); got != 17 {
			t.Fatalf("percentage changed to %v for line %q", got, line)
		}
	}
}
