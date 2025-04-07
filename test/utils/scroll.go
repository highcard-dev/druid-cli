package test_utils

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"gopkg.in/yaml.v2"
)

func WriteScroll(scroll domain.File, path string) error {

	b, err := yaml.Marshal(scroll)
	if err != nil {
		return err
	}

	return os.WriteFile(path, b, 0644)
}

func SetupScroll(t *testing.T, scroll domain.File) (string, string) {

	logger.Log(logger.WithStructuredLogging())

	//observer := logger.SetupLogsCapture()
	unixTime := time.Now().Unix()
	cwd := "./druid-cli-test/" + strconv.FormatInt(unixTime, 10) + "/"
	scrollPath := cwd + ".scroll/"

	t.Logf("Creating test scroll file in %s", scrollPath)

	if err := os.MkdirAll(scrollPath, 0755); err != nil {
		t.Fatalf("Failed to create test cwd: %v", err)
	}

	err := WriteScroll(scroll, scrollPath+"scroll.yaml")
	if err != nil {
		t.Fatalf("Failed to write test scroll file: %v", err)
	}
	return scrollPath, cwd
}
