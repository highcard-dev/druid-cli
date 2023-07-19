package logger

import (
	"github.com/highcard-dev/daemon/internal/core/ports"
	drivers "github.com/highcard-dev/daemon/internal/core/services/log/drivers"
)

var logger ports.LogDriverInterface

func NewLogger(mode string) ports.LogDriverInterface {

	if mode == "cli" {
		logger = drivers.NewCliLogDriver()
	} else {
		logger = drivers.NewStructuredLogDriver()
	}

	return logger
}

func Log() ports.LogDriverInterface {
	if logger == nil {
		logger = NewLogger("structured")
	}
	return logger
}
