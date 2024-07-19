package domain

import (
	"encoding/json"
	"os"

	"github.com/Masterminds/semver/v3"
)

type ScrollLockStatus string

const (
	ScrollLockStatusRunning ScrollLockStatus = "running"
	ScrollLockStatusDone    ScrollLockStatus = "done"
	ScrollLockStatusError   ScrollLockStatus = "error"
	ScrollLockStatusWaiting ScrollLockStatus = "waiting"
)

type ScrollLock struct {
	Statuses      map[string]ScrollLockStatus `json:"statuses"`
	ScrollVersion *semver.Version             `json:"scroll_version"`
	ScrollName    string                      `json:"scroll_name"`
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

func ReadLock(path string) (*ScrollLock, error) {
	lock := &ScrollLock{}

	scrollRaw, _ := os.ReadFile(path)
	err := json.Unmarshal(scrollRaw, &lock)
	if err != nil {
		return nil, err
	}
	lock.path = path
	return lock, nil
}

func WriteNewScrollLock(path string) *ScrollLock {
	lock := &ScrollLock{
		Statuses: make(map[string]ScrollLockStatus),
		path:     path,
	}
	lock.Write()
	return lock
}

func (scrollLock *ScrollLock) GetStatus(command string) ScrollLockStatus {
	return scrollLock.Statuses[command]
}

func (scrollLock *ScrollLock) SetStatus(command string, status ScrollLockStatus) {
	scrollLock.Statuses[command] = status
	scrollLock.Write()
}
