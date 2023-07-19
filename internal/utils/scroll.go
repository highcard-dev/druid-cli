package utils

import "path/filepath"

func GetScrollDirFromCwd(cwd string) string {
	return filepath.Join(cwd, ".scroll")
}
