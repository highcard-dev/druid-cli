package utils

import "path/filepath"

func GetDataDirFromScrollDir(cwd string) string {
	return filepath.Join(cwd, "data")
}
