package domain

import (
	"encoding/json"
	"os"
	"time"

	"github.com/Masterminds/semver/v3"
)

type ScrollLockStatus string

const (
	ScrollLockStatusRunning ScrollLockStatus = "running"
	ScrollLockStatusDone    ScrollLockStatus = "done"
	ScrollLockStatusError   ScrollLockStatus = "error"
	ScrollLockStatusWaiting ScrollLockStatus = "waiting"
)

type LockStatus struct {
	Status           ScrollLockStatus `json:"status"`
	ExitCode         *int             `json:"exit_code"`
	LastStatusChange int64            `json:"last_status_change"`
}

type ScrollLock struct {
	Statuses      map[string]LockStatus `json:"statuses"`
	ScrollVersion *semver.Version       `json:"scroll_version"`
	ScrollName    string                `json:"scroll_name"`
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
		Statuses: make(map[string]LockStatus),
		path:     path,
	}
	lock.Write()
	return lock
}

func (scrollLock *ScrollLock) GetStatus(command string) LockStatus {
	return scrollLock.Statuses[command]
}

func (scrollLock *ScrollLock) SetStatus(command string, status ScrollLockStatus, exitCode *int) {
	lockStatus := LockStatus{
		Status:           status,
		LastStatusChange: time.Now().Unix(),
		ExitCode:         exitCode,
	}

	scrollLock.Statuses[command] = lockStatus
	scrollLock.Write()
}
