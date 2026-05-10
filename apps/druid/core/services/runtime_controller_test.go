package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	runtimekubernetes "github.com/highcard-dev/daemon/internal/runtime/kubernetes"
)

func TestRuntimeSessionUsesCachedScrollYAML(t *testing.T) {
	scrollRoot := t.TempDir()
	dataRoot := filepath.Join(t.TempDir(), "data")
	runtimeScroll := &domain.RuntimeScroll{
		ID:         "cached",
		Artifact:   "local",
		ScrollRoot: scrollRoot,
		DataRoot:   dataRoot,
		ScrollName: "cached",
		ScrollYAML: `name: cached
desc: Cached scroll
version: 0.1.0
app_version: "1.0"
serve: start
commands:
  start:
    procedures:
      - image: alpine:3.20
        command: ["true"]
`,
	}

	session, err := NewRuntimeSession(coreservices.NewRuntimeStateStore(t.TempDir()), runtimeScroll, coreservices.NewConsoleManager(coreservices.NewLogManager()), "docker")
	if err != nil {
		t.Fatal(err)
	}
	if got := session.scrollService.GetFile().Name; got != "cached" {
		t.Fatalf("scroll name = %q, want cached", got)
	}
}

func TestRuntimeSessionHydrateAutoStartsServeWithoutPreviousStatus(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, cachedScrollYAML("start"))

	if err := session.Hydrate(); err != nil {
		t.Fatal(err)
	}

	assertQueued(t, session, "start")
}

func TestRuntimeSessionHydrateForceRequeuesDoneServe(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{
		"start": {Status: domain.ScrollLockStatusDone},
	}, cachedScrollYAML("start"))

	if err := session.Hydrate(); err != nil {
		t.Fatal(err)
	}

	assertQueued(t, session, "start")
}

func TestRuntimeSessionHydrateRequeuesErrorServe(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{
		"start": {Status: domain.ScrollLockStatusError},
	}, cachedScrollYAML("start"))

	if err := session.Hydrate(); err != nil {
		t.Fatal(err)
	}

	assertQueued(t, session, "start")
}

func TestRuntimeSessionHydrateDoesNotDuplicateActiveServe(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{
		"start": {Status: domain.ScrollLockStatusRunning},
	}, cachedScrollYAML("start"))

	if err := session.Hydrate(); err != nil {
		t.Fatal(err)
	}

	queue := session.queueManager.GetQueue()
	if len(queue) != 1 {
		t.Fatalf("queue len = %d, want 1: %#v", len(queue), queue)
	}
	if queue["start"] != domain.ScrollLockStatusWaiting {
		t.Fatalf("start = %s, want waiting", queue["start"])
	}
}

func TestRuntimeSessionHydrateSkipsMissingServe(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, cachedScrollYAML(""))

	if err := session.Hydrate(); err != nil {
		t.Fatal(err)
	}

	if queue := session.queueManager.GetQueue(); len(queue) != 0 {
		t.Fatalf("queue = %#v, want empty", queue)
	}
}

func TestRuntimeSessionHydrateDropsStaleCommandStatus(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{
		"missing": {Status: domain.ScrollLockStatusDone},
	}, cachedScrollYAML(""))

	if err := session.Hydrate(); err != nil {
		t.Fatal(err)
	}

	updated, err := session.store.GetScroll(session.runtimeScroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := updated.Commands["missing"]; ok {
		t.Fatalf("stale command was not removed: %#v", updated.Commands)
	}
}

func TestRuntimeSessionHydrateDoesNotRequeueRunningPersistentServe(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{
		"start": {Status: domain.ScrollLockStatusDone},
	}, `name: cached
desc: Cached scroll
version: 0.1.0
app_version: "1.0"
serve: start
commands:
  start:
    run: persistent
    procedures:
      - image: alpine:3.20
        command: ["true"]
`)
	session.runtimeScroll.Status = domain.RuntimeScrollStatusRunning
	if err := session.store.UpdateScroll(session.runtimeScroll); err != nil {
		t.Fatal(err)
	}

	if err := session.Hydrate(); err != nil {
		t.Fatal(err)
	}

	if queue := session.queueManager.GetQueue(); len(queue) != 0 {
		t.Fatalf("queue = %#v, want empty", queue)
	}
}

func TestRuntimeSessionAutoStartsServeOnCreatePath(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, cachedScrollYAML("start"))

	if err := session.AutoStartServe(); err != nil {
		t.Fatal(err)
	}

	assertQueued(t, session, "start")
}

func TestRuntimeSupervisorEnsureCanCreateWithoutStarting(t *testing.T) {
	scrollRoot := t.TempDir()
	dataRoot := scrollRoot
	if err := os.WriteFile(filepath.Join(scrollRoot, "scroll.yaml"), []byte(cachedScrollYAML("start")), 0644); err != nil {
		t.Fatal(err)
	}
	store := coreservices.NewRuntimeStateStore(t.TempDir())
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		coreservices.NewConsoleManager(coreservices.NewLogManager()),
		"docker",
	)

	runtimeScroll, err := supervisor.Ensure("local", "quiet-scroll", scrollRoot, dataRoot, false)
	if err != nil {
		t.Fatal(err)
	}

	if runtimeScroll.Status != domain.RuntimeScrollStatusCreated {
		t.Fatalf("status = %s, want created", runtimeScroll.Status)
	}
	if len(runtimeScroll.Commands) != 0 {
		t.Fatalf("commands = %#v, want empty", runtimeScroll.Commands)
	}
}

func TestRuntimeSupervisorCreateCanCreateWithoutStarting(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "scroll.yaml"), []byte(cachedScrollYAML("start")), 0644); err != nil {
		t.Fatal(err)
	}
	store := coreservices.NewRuntimeStateStore(t.TempDir())
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		coreservices.NewConsoleManager(coreservices.NewLogManager()),
		"docker",
	)

	runtimeScroll, err := supervisor.Create("local", "quiet-create", root, root, false)
	if err != nil {
		t.Fatal(err)
	}

	if runtimeScroll.Status != domain.RuntimeScrollStatusCreated {
		t.Fatalf("status = %s, want created", runtimeScroll.Status)
	}
	if len(runtimeScroll.Commands) != 0 {
		t.Fatalf("commands = %#v, want empty", runtimeScroll.Commands)
	}
}

func TestRuntimeSessionApplyRoutingPersistsAssignments(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, cachedScrollYAML(""))

	updated, err := session.ApplyRouting([]domain.RuntimeRouteAssignment{{
		Name:       "web-http",
		PortName:   "http",
		Host:       "scroll.example.test",
		PublicPort: 443,
		URL:        "https://scroll.example.test",
		Protocol:   "https",
	}})
	if err != nil {
		t.Fatal(err)
	}

	if len(updated.Routing) != 1 || updated.Routing[0].Host != "scroll.example.test" {
		t.Fatalf("routing = %#v", updated.Routing)
	}
}

func TestDeriveRuntimeScrollStatusTreatsDonePersistentAsRunning(t *testing.T) {
	status := deriveRuntimeScrollStatus(map[string]domain.LockStatus{
		"start": {Status: domain.ScrollLockStatusDone},
	}, map[string]*domain.CommandInstructionSet{
		"start": {Run: domain.RunModePersistent},
	})

	if status != domain.RuntimeScrollStatusRunning {
		t.Fatalf("status = %s, want running", status)
	}
}

func TestDeriveRuntimeScrollStatusTreatsDoneFiniteAsStopped(t *testing.T) {
	status := deriveRuntimeScrollStatus(map[string]domain.LockStatus{
		"report": {Status: domain.ScrollLockStatusDone},
	}, map[string]*domain.CommandInstructionSet{
		"report": {Run: domain.RunModeAlways},
	})

	if status != domain.RuntimeScrollStatusStopped {
		t.Fatalf("status = %s, want stopped", status)
	}
}

func TestNewRuntimeStoreForBackendUsesKubernetesStoreWithoutStateDB(t *testing.T) {
	stateDir := t.TempDir()
	called := false
	previous := newKubernetesRuntimeStore
	newKubernetesRuntimeStore = func(config runtimekubernetes.Config) (coreservices.RuntimeScrollStore, error) {
		called = true
		if config.Namespace != "druid" {
			t.Fatalf("namespace = %s, want druid", config.Namespace)
		}
		return fakeRuntimeScrollStore{state: "kubernetes:druid/configmaps"}, nil
	}
	t.Cleanup(func() {
		newKubernetesRuntimeStore = previous
	})

	store, err := NewRuntimeStoreForBackend(stateDir, "kubernetes", runtimekubernetes.Config{Namespace: "druid"})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("kubernetes store factory was not called")
	}
	if store.StateDir() != "kubernetes:druid/configmaps" {
		t.Fatalf("StateDir = %s, want kubernetes:druid/configmaps", store.StateDir())
	}
	if _, err := os.Stat(filepath.Join(stateDir, "state.db")); !os.IsNotExist(err) {
		t.Fatalf("state.db stat error = %v, want not exist", err)
	}
}

func TestWriteRuntimeConfigSkipsKubernetesRefs(t *testing.T) {
	workingDir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previous)
	})

	err = WriteRuntimeConfig(&domain.RuntimeScroll{
		ID:         "container-lab",
		Artifact:   "artifact",
		DataRoot:   "k8s://druid/druid-container-lab-data",
		ScrollName: "container-lab",
	}, &domain.File{}, "kubernetes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workingDir, "k8s:")); !os.IsNotExist(err) {
		t.Fatalf("k8s: stat error = %v, want not exist", err)
	}
}

func newRuntimeSessionForTest(t *testing.T, commands map[string]domain.LockStatus, scrollYAML string) *RuntimeSession {
	t.Helper()
	scrollRoot := t.TempDir()
	dataRoot := filepath.Join(t.TempDir(), "data")
	store := coreservices.NewRuntimeStateStore(t.TempDir())
	runtimeScroll := &domain.RuntimeScroll{
		ID:         "cached",
		Artifact:   "local",
		ScrollRoot: scrollRoot,
		DataRoot:   dataRoot,
		ScrollName: "cached",
		ScrollYAML: scrollYAML,
		Commands:   commands,
	}
	if err := store.CreateScroll(runtimeScroll); err != nil {
		t.Fatal(err)
	}
	session, err := NewRuntimeSession(store, runtimeScroll, coreservices.NewConsoleManager(coreservices.NewLogManager()), "docker")
	if err != nil {
		t.Fatal(err)
	}
	return session
}

type fakeRuntimeScrollStore struct {
	state string
}

func (f fakeRuntimeScrollStore) StateDir() string {
	return f.state
}

func (f fakeRuntimeScrollStore) ScrollRoot(id string) string {
	return ""
}

func (f fakeRuntimeScrollStore) DataRoot(id string) string {
	return ""
}

func (f fakeRuntimeScrollStore) CreateScroll(scroll *domain.RuntimeScroll) error {
	return nil
}

func (f fakeRuntimeScrollStore) ListScrolls() ([]*domain.RuntimeScroll, error) {
	return nil, nil
}

func (f fakeRuntimeScrollStore) GetScroll(id string) (*domain.RuntimeScroll, error) {
	return nil, coreservices.ErrScrollNotFound
}

func (f fakeRuntimeScrollStore) UpdateScroll(scroll *domain.RuntimeScroll) error {
	return nil
}

func (f fakeRuntimeScrollStore) DeleteScroll(id string) error {
	return nil
}

func cachedScrollYAML(serve string) string {
	yaml := `name: cached
desc: Cached scroll
version: 0.1.0
app_version: "1.0"
`
	if serve != "" {
		yaml += "serve: " + serve + "\n"
	}
	yaml += `commands:
  start:
    run: once
    procedures:
      - image: alpine:3.20
        command: ["true"]
`
	return yaml
}

func assertQueued(t *testing.T, session *RuntimeSession, command string) {
	t.Helper()
	queue := session.queueManager.GetQueue()
	if queue[command] != domain.ScrollLockStatusWaiting {
		t.Fatalf("%s = %s, want waiting; queue=%#v", command, queue[command], queue)
	}
}
