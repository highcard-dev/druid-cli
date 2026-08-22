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
	create  ports.RuntimeBackendFactoryFunc
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

var newDockerBackend = func(config docker.Config, consoleManager ports.ConsoleManagerInterface, observer ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
	return docker.NewWithConfig(config, consoleManager, observer)
}

var newKubernetesBackend = func(config runtimekubernetes.Config, consoleManager ports.ConsoleManagerInterface, observer ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
	return runtimekubernetes.New(config, consoleManager, observer)
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
		store, err := docker.NewStateStore(stateDir)
		if err != nil {
			return nil, err
		}
		return &Runtime{
			Store: dockerRuntimeStore{RuntimeScrollStore: store, config: options.Docker.WithDefaults()},
			create: func(observer ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
				return newDockerBackend(options.Docker, consoleManager, observer)
			},
		}, nil
	case "kubernetes":
		store, err := newKubernetesStateStore(options.Kubernetes)
		if err != nil {
			return nil, err
		}
		return &Runtime{
			Store: store,
			create: func(observer ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
				return newKubernetesBackend(options.Kubernetes, consoleManager, observer)
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown runtime backend %q", name)
	}
}

func (r *Runtime) Create(observer ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
	if observer == nil {
		return nil, fmt.Errorf("procedure status observer is required")
	}
	if r.Backend != nil {
		return nil, fmt.Errorf("runtime backend already created")
	}
	if r.create == nil {
		return nil, fmt.Errorf("runtime backend factory is not configured")
	}
	backend, err := r.create(observer)
	if err != nil {
		return nil, err
	}
	if backend == nil {
		return nil, fmt.Errorf("runtime backend factory returned nil")
	}
	r.Backend = backend
	return backend, nil
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
