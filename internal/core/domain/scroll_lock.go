package domain

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/Masterminds/semver/v3"
	"github.com/highcard-dev/daemon/internal/utils"
)

type ScrollLock struct {
	Statuses      map[string]string `json:"statuses"`
	ScrollVersion *semver.Version   `json:"scroll_version"`
	ScrollName    string            `json:"scroll_name"`
	Initialized   bool              `json:"initialized"`
	path          string
} // @name ScrollLock

func (scrollLock *ScrollLock) Write() error {
	data, err := json.Marshal(scrollLock)
	if err != nil {
		return err
	}
	os.WriteFile(scrollLock.path, data, 0755)
	return nil
}

func (scrollLock *ScrollLock) Read() (*ScrollLock, error) {

	lock := &ScrollLock{}

	if !scrollLock.LockExists() {
		return nil, errors.New("Scroll lock not found")
	}
	scrollRaw, _ := os.ReadFile(scrollLock.path)
	err := json.Unmarshal(scrollRaw, &lock)
	if err != nil {
		return nil, err
	}
	return lock, nil
}

func (scrollLock *ScrollLock) LockExists() bool {
	exisits, err := utils.FileExists(scrollLock.path)
	return err == nil && exisits
}

func NewScrollLock(path string) *ScrollLock {
	return &ScrollLock{path: path}
}
