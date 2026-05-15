package docker

import (
	"net"

	"github.com/highcard-dev/daemon/internal/core/ports"
)

func (b *Backend) WorkerCallbackDefaults(config ports.RuntimeWorkerCallbackConfig) ports.RuntimeWorkerCallbackConfig {
	if config.Listen == "" && config.URL == "" {
		config.Listen = "127.0.0.1:0"
	}
	return config
}

func (b *Backend) WorkerCallbackAfterListen(config ports.RuntimeWorkerCallbackConfig) (ports.RuntimeWorkerCallbackConfig, error) {
	if config.URL != "" {
		return config, nil
	}
	_, port, err := net.SplitHostPort(config.Listen)
	if err != nil {
		return config, err
	}
	config.URL = "http://host.docker.internal:" + port
	return config, nil
}
