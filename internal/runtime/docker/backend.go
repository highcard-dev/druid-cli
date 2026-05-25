package docker

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/docker/docker/client"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type Backend struct {
	client         *client.Client
	consoleManager ports.ConsoleManagerInterface
	config         Config
	mu             sync.Mutex
	containers     map[string]string
	stdin          map[string]io.Writer
}

type Config struct {
	WorkerImage  string
	Network      string
	Storage      string
	BindRoot     string
	VolumePrefix string
	UIBind       string
	UIPublicURL  string
}

func (c Config) WithDefaults() Config {
	if c.WorkerImage == "" {
		c.WorkerImage = os.Getenv("DRUID_DOCKER_WORKER_IMAGE")
	}
	if c.Network == "" {
		c.Network = os.Getenv("DRUID_DOCKER_NETWORK")
	}
	if c.Storage == "" {
		c.Storage = os.Getenv("DRUID_DOCKER_STORAGE")
	}
	if c.Storage == "" {
		c.Storage = StorageVolume
	}
	if c.BindRoot == "" {
		c.BindRoot = os.Getenv("DRUID_DOCKER_BIND_ROOT")
	}
	if c.VolumePrefix == "" {
		c.VolumePrefix = os.Getenv("DRUID_DOCKER_VOLUME_PREFIX")
	}
	if c.VolumePrefix == "" {
		c.VolumePrefix = "druid"
	}
	if c.UIBind == "" {
		c.UIBind = os.Getenv("DRUID_DOCKER_UI_BIND")
	}
	if c.UIBind == "" {
		c.UIBind = "127.0.0.1:8085"
	}
	if c.UIPublicURL == "" {
		c.UIPublicURL = os.Getenv("DRUID_DOCKER_UI_PUBLIC_URL")
	}
	if c.UIPublicURL == "" {
		c.UIPublicURL = "http://" + c.UIBind
	}
	return c
}

func New(consoleManager ports.ConsoleManagerInterface) (*Backend, error) {
	return NewWithConfig(Config{}, consoleManager)
}

func NewWithConfig(config Config, consoleManager ports.ConsoleManagerInterface) (*Backend, error) {
	config = config.WithDefaults()
	if config.Storage != StorageVolume && config.Storage != StorageBind {
		return nil, fmt.Errorf("unknown docker storage %q", config.Storage)
	}
	if config.Storage == StorageBind {
		if config.BindRoot == "" {
			return nil, fmt.Errorf("docker bind root is required when docker storage is bind")
		}
		if !filepath.IsAbs(config.BindRoot) {
			return nil, fmt.Errorf("docker bind root must be absolute: %s", config.BindRoot)
		}
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Backend{
		client:         cli,
		consoleManager: consoleManager,
		config:         config,
		containers:     map[string]string{},
		stdin:          map[string]io.Writer{},
	}, nil
}

func (b *Backend) Name() string {
	return "docker"
}

func (b *Backend) RootRef(id string, _ string) string {
	root, err := b.config.RuntimeRootRef(id)
	if err != nil {
		return id
	}
	return root
}
