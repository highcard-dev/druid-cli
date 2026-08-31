package runtime

import (
	"context"
	"io"
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
	newDockerBackend = func(config docker.Config, observer ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
		if config.VolumePrefix != "lab" {
			t.Fatalf("volume prefix = %s, want lab", config.VolumePrefix)
		}
		if observer == nil {
			t.Fatal("procedure status observer is nil")
		}
		return fakeBackend{name: "docker"}, nil
	}
	t.Cleanup(func() { newDockerBackend = previousDocker })

	runtime, err := NewRuntime("docker", t.TempDir(), WithDockerConfig(docker.Config{VolumePrefix: "lab"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Create(ports.ProcedureStatusObserverFunc(func(ports.ProcedureStatusUpdate) {})); err != nil {
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
	newKubernetesBackend = func(config runtimekubernetes.Config, observer ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
		if config.Namespace != "druid" {
			t.Fatalf("backend namespace = %s, want druid", config.Namespace)
		}
		if observer == nil {
			t.Fatal("procedure status observer is nil")
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

	runtime, err := NewRuntime("kubernetes", stateDir, WithKubernetesConfig(runtimekubernetes.Config{Namespace: "druid"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Create(ports.ProcedureStatusObserverFunc(func(ports.ProcedureStatusUpdate) {})); err != nil {
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
	_, err := NewRuntime("nope", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `unknown runtime backend "nope"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestRuntimeCreateRequiresObserver(t *testing.T) {
	called := false
	runtime := &Runtime{create: func(observer ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
		called = true
		return fakeBackend{name: "docker"}, nil
	}}

	if _, err := runtime.Create(nil); err == nil || !strings.Contains(err.Error(), "observer is required") {
		t.Fatalf("Create error = %v, want required observer", err)
	}
	if called {
		t.Fatal("backend creator called without an observer")
	}
}

func TestRuntimeCreateOnlyCreatesBackendOnce(t *testing.T) {
	runtime := &Runtime{create: func(observer ports.ProcedureStatusObserver) (ports.RuntimeBackendInterface, error) {
		return fakeBackend{name: "docker"}, nil
	}}
	observer := ports.ProcedureStatusObserverFunc(func(ports.ProcedureStatusUpdate) {})
	if _, err := runtime.Create(observer); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Create(observer); err == nil || !strings.Contains(err.Error(), "already created") {
		t.Fatalf("second Create error = %v, want already created", err)
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

func (f fakeBackend) RunCommand(command ports.RuntimeCommand) (*int, error) {
	return nil, nil
}

func (f fakeBackend) CreateUIPackageUpload(ctx context.Context, action ports.RuntimeUIPackageUploadAction) (ports.RuntimeUIPackageUpload, error) {
	return ports.RuntimeUIPackageUpload{}, nil
}

func (f fakeBackend) ExpectedPorts(root string, commands map[string]*domain.CommandInstructionSet, ports []domain.Port) ([]domain.RuntimePortStatus, error) {
	return nil, nil
}

func (f fakeBackend) RoutingTargets(root string, commands map[string]*domain.CommandInstructionSet, ports []domain.Port) ([]domain.RuntimeRoutingTarget, error) {
	return nil, nil
}

func (f fakeBackend) StopCommand(root string, command string) error {
	return nil
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

func (f fakeBackend) SpawnPullWorker(ctx context.Context, action ports.RuntimeWorkerAction) (<-chan error, error) {
	done := make(chan error, 1)
	done <- nil
	return done, nil
}

func (f fakeBackend) OpenConsole(context.Context, string, string) (io.ReadWriteCloser, error) {
	return nil, nil
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
