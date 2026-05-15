package runtime

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/runtime/docker"
	runtimekubernetes "github.com/highcard-dev/daemon/internal/runtime/kubernetes"
)

type Runtime struct {
	Backend ports.RuntimeBackendInterface
	Store   ports.RuntimeScrollStore
}

type Options struct {
	Kubernetes runtimekubernetes.Config
	Docker     docker.Config
}

type Option func(*Options)

func WithKubernetesConfig(config runtimekubernetes.Config) Option {
	return func(options *Options) {
		options.Kubernetes = config
	}
}

func WithDockerConfig(config docker.Config) Option {
	return func(options *Options) {
		options.Docker = config
	}
}

var newDockerBackend = func(config docker.Config, consoleManager ports.ConsoleManagerInterface) (ports.RuntimeBackendInterface, error) {
	return docker.NewWithConfig(config, consoleManager)
}

var newKubernetesBackend = func(config runtimekubernetes.Config, consoleManager ports.ConsoleManagerInterface) (ports.RuntimeBackendInterface, error) {
	return runtimekubernetes.New(config, consoleManager)
}

var newKubernetesStateStore = func(config runtimekubernetes.Config) (ports.RuntimeScrollStore, error) {
	return runtimekubernetes.NewConfigMapStateStore(config)
}

func NewRuntime(name string, consoleManager ports.ConsoleManagerInterface, stateDir string, opts ...Option) (*Runtime, error) {
	options := Options{}
	for _, opt := range opts {
		opt(&options)
	}
	switch name {
	case "", "docker":
		backend, err := newDockerBackend(options.Docker, consoleManager)
		if err != nil {
			return nil, err
		}
		store, err := docker.NewStateStore(stateDir)
		if err != nil {
			return nil, err
		}
		return &Runtime{
			Backend: backend,
			Store:   dockerRuntimeStore{RuntimeScrollStore: store, config: options.Docker.WithDefaults()},
		}, nil
	case "kubernetes":
		backend, err := newKubernetesBackend(options.Kubernetes, consoleManager)
		if err != nil {
			return nil, err
		}
		store, err := newKubernetesStateStore(options.Kubernetes)
		if err != nil {
			return nil, err
		}
		return &Runtime{Backend: backend, Store: store}, nil
	default:
		return nil, fmt.Errorf("unknown runtime backend %q", name)
	}
}

type dockerRuntimeStore struct {
	ports.RuntimeScrollStore
	config docker.Config
}

func (s dockerRuntimeStore) Root(id string) string {
	root, err := s.config.RuntimeRootRef(id)
	if err != nil {
		return s.RuntimeScrollStore.Root(id)
	}
	return root
}
