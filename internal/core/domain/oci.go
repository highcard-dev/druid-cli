package domain

import "sync/atomic"

type ArtifactType string

const (
	ArtifactTypeScrollRoot ArtifactType = "application/vnd.highcard.druid.scroll.config.v1+json"
	ArtifactTypeScrollFs   ArtifactType = "application/vnd.highcard.druid.scroll-fs.config.v1+json"
	ArtifactTypeScrollData ArtifactType = "application/vnd.highcard.druid.scroll-data.config.v1+json"
	ArtifactTypeScrollMeta ArtifactType = "application/vnd.highcard.druid.scroll-meta.config.v1+json"
)

// SnapshotProgress tracks the state of a data pull/push operation.
// Mode values: "noop" (idle), "backup" (pushing data), "restore" (pulling data chunks).
type SnapshotProgress struct {
	Percentage atomic.Int64
	Mode       atomic.Value // stores string
}

func NewSnapshotProgress() *SnapshotProgress {
	sp := &SnapshotProgress{}
	sp.Mode.Store("noop")
	return sp
}

type AnnotationInfo struct {
	MinRam  string
	MinDisk string
	MinCpu  string
	Image   string
	Smart   bool
	Ports   map[string]string
}
