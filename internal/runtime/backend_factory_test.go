package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/runtime/docker"
	runtimekubernetes "github.com/highcard-dev/daemon/internal/runtime/kubernetes"
)

func TestNewRuntimeDockerOwnsStoreSelection(t *testing.T) {
	previousDocker := newDockerBackend
	newDockerBackend = func(config docker.Config, consoleManager ports.ConsoleManagerInterface) (ports.RuntimeBackendInterface, error) {
		if config.VolumePrefix != "lab" {
			t.Fatalf("volume prefix = %s, want lab", config.VolumePrefix)
		}
		return fakeBackend{name: "docker"}, nil
	}
	t.Cleanup(func() { newDockerBackend = previousDocker })

	runtime, err := NewRuntime("docker", nil, t.TempDir(), WithDockerConfig(docker.Config{VolumePrefix: "lab"}))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Backend.Name() != "docker" {
		t.Fatalf("backend = %s, want docker", runtime.Backend.Name())
	}
	if got := runtime.Store.Root("scroll-a"); got != "docker-volume://lab-scroll-a-data" {
		t.Fatalf("Root = %s", got)
	}
}

func TestNewRuntimeKubernetesOwnsStoreSelection(t *testing.T) {
	stateDir := t.TempDir()
	previousBackend := newKubernetesBackend
	previousStore := newKubernetesStateStore
	newKubernetesBackend = func(config runtimekubernetes.Config, consoleManager ports.ConsoleManagerInterface) (ports.RuntimeBackendInterface, error) {
		if config.Namespace != "druid" {
			t.Fatalf("backend namespace = %s, want druid", config.Namespace)
		}
		return fakeBackend{name: "kubernetes"}, nil
	}
	newKubernetesStateStore = func(config runtimekubernetes.Config) (ports.RuntimeScrollStore, error) {
		if config.Namespace != "druid" {
			t.Fatalf("store namespace = %s, want druid", config.Namespace)
		}
		return fakeStore{state: "kubernetes:druid/configmaps"}, nil
	}
	t.Cleanup(func() {
		newKubernetesBackend = previousBackend
		newKubernetesStateStore = previousStore
	})

	runtime, err := NewRuntime("kubernetes", nil, stateDir, WithKubernetesConfig(runtimekubernetes.Config{Namespace: "druid"}))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Backend.Name() != "kubernetes" {
		t.Fatalf("backend = %s, want kubernetes", runtime.Backend.Name())
	}
	if runtime.Store.StateDir() != "kubernetes:druid/configmaps" {
		t.Fatalf("StateDir = %s, want kubernetes:druid/configmaps", runtime.Store.StateDir())
	}
	if _, err := os.Stat(filepath.Join(stateDir, "state.db")); !os.IsNotExist(err) {
		t.Fatalf("state.db stat error = %v, want not exist", err)
	}
}

func TestNewRuntimeUnknownBackendErrorsOnce(t *testing.T) {
	_, err := NewRuntime("nope", nil, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `unknown runtime backend "nope"`) {
		t.Fatalf("error = %v", err)
	}
}

type fakeBackend struct {
	name string
}

func (f fakeBackend) Name() string {
	return f.name
}

func (f fakeBackend) RootRef(id string, namespace string) string {
	if namespace != "" {
		return namespace + "/" + id
	}
	return id
}

func (f fakeBackend) StartDev(ctx context.Context, action ports.RuntimeDevAction) error {
	return nil
}

func (f fakeBackend) StopDev(ctx context.Context, root string) error { return nil }

func (f fakeBackend) RunCommand(command ports.RuntimeCommand) (*int, error) {
	return nil, nil
}

func (f fakeBackend) PublishUIPackage(ctx context.Context, action ports.RuntimeUIPackageAction) (ports.RuntimeUIPackageResult, error) {
	return ports.RuntimeUIPackageResult{}, nil
}

func (f fakeBackend) ExpectedPorts(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimePortStatus, error) {
	return nil, nil
}

func (f fakeBackend) RoutingTargets(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimeRoutingTarget, error) {
	return nil, nil
}

func (f fakeBackend) StopRuntime(root string) error {
	return nil
}

func (f fakeBackend) DeleteRuntime(root string, purgeData bool) error {
	return nil
}

func (f fakeBackend) BackupRuntime(ctx context.Context, root string, artifact string, registryCredentials []domain.RegistryCredential) error {
	return nil
}

func (f fakeBackend) SpawnPullWorker(ctx context.Context, action ports.RuntimeWorkerAction) error {
	return nil
}

func (f fakeBackend) Attach(commandName string, data string) error {
	return nil
}

func (f fakeBackend) Signal(commandName string, target string, signal string, root string) error {
	return nil
}

type fakeStore struct {
	state string
}

func (f fakeStore) StateDir() string {
	return f.state
}

func (f fakeStore) Root(id string) string {
	return ""
}

func (f fakeStore) CreateScroll(scroll *domain.RuntimeScroll) error {
	return nil
}

func (f fakeStore) ListScrolls() ([]*domain.RuntimeScroll, error) {
	return nil, nil
}

func (f fakeStore) GetScroll(id string) (*domain.RuntimeScroll, error) {
	return nil, domain.ErrRuntimeScrollNotFound
}

func (f fakeStore) UpdateScroll(scroll *domain.RuntimeScroll) error {
	return nil
}

func (f fakeStore) DeleteScroll(id string) error {
	return nil
}
