package domain

import (
	"math"
	"sync/atomic"
)

type ArtifactType string

const (
	ArtifactTypeRuntimeRoot ArtifactType = "application/vnd.highcard.druid.scroll.config.v1+json"
	ArtifactTypeScrollFs    ArtifactType = "application/vnd.highcard.druid.scroll-fs.config.v1+json"
	ArtifactTypeScrollData  ArtifactType = "application/vnd.highcard.druid.scroll-data.config.v1+json"
	ArtifactTypeScrollMeta  ArtifactType = "application/vnd.highcard.druid.scroll-meta.config.v1+json"
)

const (
	SnapshotProgressModeIdle    = "idle"
	SnapshotProgressModeBackup  = "backup"
	SnapshotProgressModeRestore = "restore"
)

const snapshotProgressScale int64 = 10

// SnapshotProgress tracks the state of a data pull/push operation.
type SnapshotProgress struct {
	percentage atomic.Int64
	Mode       atomic.Value // stores string
}

func NewSnapshotProgress() *SnapshotProgress {
	sp := &SnapshotProgress{}
	sp.Mode.Store(SnapshotProgressModeIdle)
	return sp
}

func (p *SnapshotProgress) StorePercentage(value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return false
	}
	value = math.Max(0, math.Min(100, value))
	p.percentage.Store(int64(math.Round(value * float64(snapshotProgressScale))))
	return true
}

func (p *SnapshotProgress) Percentage() float64 {
	return float64(p.percentage.Load()) / float64(snapshotProgressScale)
}

type AnnotationInfo struct {
	MinRam  string
	MinDisk string
	MinCpu  string
	Image   string
	Smart   bool
	Ports   map[string]string
}
