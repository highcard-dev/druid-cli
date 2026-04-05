package utils

import (
	"path/filepath"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func GetDataDirFromScrollDir(cwd string) string {
	return filepath.Join(cwd, domain.ScrollDataDir)
}
