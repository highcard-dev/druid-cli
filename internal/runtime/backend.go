package runtime

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/runtime/docker"
	runtimekubernetes "github.com/highcard-dev/daemon/internal/runtime/kubernetes"
)

type Options struct {
	Kubernetes runtimekubernetes.Config
}

type Option func(*Options)

func WithKubernetesConfig(config runtimekubernetes.Config) Option {
	return func(options *Options) {
		options.Kubernetes = config
	}
}

func NewBackend(name string, consoleManager ports.ConsoleManagerInterface, opts ...Option) (ports.RuntimeBackendInterface, error) {
	options := Options{}
	for _, opt := range opts {
		opt(&options)
	}
	switch name {
	case "", "docker":
		return docker.New(consoleManager)
	case "kubernetes":
		return runtimekubernetes.New(options.Kubernetes, consoleManager)
	default:
		return nil, fmt.Errorf("unknown runtime backend %q", name)
	}
}
