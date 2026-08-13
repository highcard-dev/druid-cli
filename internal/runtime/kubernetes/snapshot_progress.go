package kubernetes

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

const snapshotProgressPrefix = "DRUID_PROGRESS_V1 "

var steamCMDProgressPattern = regexp.MustCompile(
	`Update state \(0x[0-9a-fA-F]+\) downloading, progress: [0-9]+(?:\.[0-9]+)? \(([0-9]+) / ([0-9]+)\)`,
)

type snapshotProgressSample struct {
	Unit    string  `json:"unit"`
	Current float64 `json:"current"`
	Total   float64 `json:"total"`
}

func observeSnapshotProgress(line string, progress *domain.SnapshotProgress) bool {
	if progress == nil {
		return false
	}

	if strings.HasPrefix(line, snapshotProgressPrefix) {
		var sample snapshotProgressSample
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, snapshotProgressPrefix)), &sample); err != nil {
			return false
		}
		if sample.Unit != "bytes" || !storeSnapshotProgress(progress, sample.Current, sample.Total) {
			return false
		}
		return true
	}

	matches := steamCMDProgressPattern.FindStringSubmatch(line)
	if len(matches) == 3 {
		current, currentErr := strconv.ParseFloat(matches[1], 64)
		total, totalErr := strconv.ParseFloat(matches[2], 64)
		if currentErr == nil && totalErr == nil {
			storeSnapshotProgress(progress, current, total)
		}
	}
	return false
}

func storeSnapshotProgress(progress *domain.SnapshotProgress, current float64, total float64) bool {
	if current < 0 || total <= 0 ||
		math.IsNaN(current) || math.IsInf(current, 0) ||
		math.IsNaN(total) || math.IsInf(total, 0) {
		return false
	}
	percentage := int64(math.Round(current / total * 100))
	progress.Percentage.Store(max(0, min(100, percentage)))
	return true
}
