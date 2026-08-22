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
	client                  *client.Client
	consoleManager          ports.ConsoleManagerInterface
	procedureStatusObserver ports.ProcedureStatusObserver
	config                  Config
	mu                      sync.Mutex
	containers              map[string]string
	stdin                   map[string]io.Writer
}

type Config struct {
	WorkerImage       string
	Network           string
	Storage           string
	BindRoot          string
	VolumePrefix      string
	UIS3Bucket        string
	UIS3PublicBaseURL string
	UIS3Region        string
	UIS3Endpoint      string
	UIS3Prefix        string
	UIS3AccessKey     string
	UIS3SecretKey     string
	UIS3SessionToken  string
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
	if c.UIS3Bucket == "" {
		c.UIS3Bucket = os.Getenv("DRUID_DOCKER_UI_S3_BUCKET")
	}
	if c.UIS3PublicBaseURL == "" {
		c.UIS3PublicBaseURL = os.Getenv("DRUID_DOCKER_UI_S3_PUBLIC_BASE_URL")
	}
	if c.UIS3Region == "" {
		c.UIS3Region = os.Getenv("DRUID_DOCKER_UI_S3_REGION")
	}
	if c.UIS3Endpoint == "" {
		c.UIS3Endpoint = os.Getenv("DRUID_DOCKER_UI_S3_ENDPOINT")
	}
	if c.UIS3Prefix == "" {
		c.UIS3Prefix = os.Getenv("DRUID_DOCKER_UI_S3_PREFIX")
	}
	if c.UIS3AccessKey == "" {
		c.UIS3AccessKey = os.Getenv("DRUID_DOCKER_UI_S3_ACCESS_KEY")
	}
	if c.UIS3SecretKey == "" {
		c.UIS3SecretKey = os.Getenv("DRUID_DOCKER_UI_S3_SECRET_KEY")
	}
	if c.UIS3SessionToken == "" {
		c.UIS3SessionToken = os.Getenv("DRUID_DOCKER_UI_S3_SESSION_TOKEN")
	}
	return c
}

func (c Config) ValidateForUIPublishing() error {
	if c.UIS3Bucket == "" || c.UIS3PublicBaseURL == "" || c.UIS3Region == "" || c.UIS3AccessKey == "" || c.UIS3SecretKey == "" {
		return fmt.Errorf("docker UI publishing requires S3 bucket, public URL, region, access key, and secret key configuration")
	}
	return nil
}

func New(consoleManager ports.ConsoleManagerInterface, observer ports.ProcedureStatusObserver) (*Backend, error) {
	return NewWithConfig(Config{}, consoleManager, observer)
}

func NewWithConfig(config Config, consoleManager ports.ConsoleManagerInterface, observer ports.ProcedureStatusObserver) (*Backend, error) {
	if observer == nil {
		return nil, fmt.Errorf("procedure status observer is required")
	}
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
		client:                  cli,
		consoleManager:          consoleManager,
		procedureStatusObserver: observer,
		config:                  config,
		containers:              map[string]string{},
		stdin:                   map[string]io.Writer{},
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
