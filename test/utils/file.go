package test_utils

import (
	"errors"
	"os"
	"time"
)

func WaitUntilFileExists(path string, duration time.Duration) error {

	timeout := time.After(duration)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return errors.New("timeout waiting for file to exist")
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				return nil
			}
		}
	}
}
