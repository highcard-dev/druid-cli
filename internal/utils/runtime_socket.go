package utils

import (
	"fmt"
	"os"
	"path/filepath"
)

func DefaultRuntimeSocketPath() string {
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "druid", "runtime.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("druid-%d-runtime.sock", os.Getuid()))
}

func DefaultRuntimeStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".druid", "runtime"), nil
}
