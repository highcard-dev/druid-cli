package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLocalFileEncoderIsDisabledWithoutLocalPath(t *testing.T) {
	t.Setenv("DRUID_LOCAL_LOG_FILE", "")

	if core := NewLocalFileEncoder(0); core != nil {
		t.Fatal("expected no local file core when DRUID_LOCAL_LOG_FILE is unset")
	}
}

func TestNewLoggerMirrorsToLocalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "druid-cli.log")
	t.Setenv("DRUID_LOCAL_LOG_FILE", path)

	log := NewLogger(WithStructuredLogging())
	log.Info("local file logging validation")
	_ = log.Sync()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read local log file: %v", err)
	}
	if !strings.Contains(string(content), "local file logging validation") {
		t.Fatalf("local log file did not contain the log entry: %s", content)
	}
}
