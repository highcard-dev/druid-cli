package registry

import (
	"github.com/Masterminds/semver/v3"
)

type Variant string
type VariantVersion string

type Entry struct {
	Latest *semver.Version `yaml:"latest"`
}

func NewRegistryEntry(latest *semver.Version) Entry {
	return Entry{Latest: latest}
}

func (re Entry) Refresh(version *semver.Version) (Entry, bool) {
	current := re.Latest
	isLatest := version.GreaterThan(current)
	isEqual := version.Equal(current)
	if isLatest || isEqual {
		re.Latest = version
	}
	return re, isLatest || isEqual
}
