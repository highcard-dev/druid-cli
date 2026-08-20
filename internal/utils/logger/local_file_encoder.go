package logger

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap/zapcore"
)

// NewLocalFileEncoder mirrors structured logs to the local file named by
// DRUID_LOCAL_LOG_FILE. The opt-in variable is set only by the local VS Code
// launch configurations; every other environment keeps its existing outputs.
func NewLocalFileEncoder(level zapcore.Level) zapcore.Core {
	path := os.Getenv("DRUID_LOCAL_LOG_FILE")
	if path == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create local log directory: %v\n", err)
		return nil
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open local log file: %v\n", err)
		return nil
	}

	return NewProductionEncoderForWriter(level, zapcore.AddSync(file))
}
