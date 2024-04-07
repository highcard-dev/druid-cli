package domain

type ArtifactType string

const (
	ArtifactTypeScrollRoot ArtifactType = "application/vnd.highcard.druid.scroll.config.v1+json"
	ArtifactTypeScrollFs   ArtifactType = "application/vnd.highcard.druid.scroll-fs.config.v1+json"
	ArtifactTypeScrollMeta ArtifactType = "application/vnd.highcard.druid.scroll-meta.config.v1+json"
)

type AnnotationInfo struct {
	MinRam  string
	MinDisk string
	MinCpu  string
	Image   string
	Ports   map[string]string
}
