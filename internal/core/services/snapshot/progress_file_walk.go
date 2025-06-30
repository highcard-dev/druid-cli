package services

import (
	"os"
	"path/filepath"
)

type ProgressFileWalk struct {
	*BasicTracker
	dir     string
	current int64
}

func NewProgressFileWalk(dir string, basicTracker *BasicTracker) *ProgressFileWalk {
	return &ProgressFileWalk{
		BasicTracker: basicTracker,
		dir:          dir,
	}
}

func (pr *ProgressFileWalk) Walk(rootPath string, f filepath.WalkFunc) error {
	return filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if info.Mode().IsRegular() {
			pr.current++
			pr.LogTrackProgress(pr.current)
		}
		return f(path, info, err)
	})
}
