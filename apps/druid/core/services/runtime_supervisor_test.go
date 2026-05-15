package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/runtime/docker"
)

func TestRuntimeSessionUsesCachedScrollYAML(t *testing.T) {
	root := t.TempDir()
	runtimeScroll := &domain.RuntimeScroll{
		ID:         "cached",
		Artifact:   "local",
		Root:       root,
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

	session, err := NewRuntimeSession(newTestStateStore(t), runtimeScroll, &fakeWorkerBackend{})
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

func TestRuntimeSupervisorEnsureCanCreate(t *testing.T) {
	artifact := t.TempDir()
	if err := os.WriteFile(filepath.Join(artifact, "scroll.yaml"), []byte(cachedScrollYAML("start")), 0644); err != nil {
		t.Fatal(err)
	}
	store := newTestStateStore(t)
	callbacks := NewWorkerCallbackManager()
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		&fakeWorkerBackend{callbacks: callbacks, scrollYAML: cachedScrollYAML("start")},
	)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	runtimeScroll, err := supervisor.Ensure(artifact, "quiet-scroll", nil)
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

func TestRuntimeSupervisorCreateCanCreate(t *testing.T) {
	artifact := t.TempDir()
	if err := os.WriteFile(filepath.Join(artifact, "scroll.yaml"), []byte(cachedScrollYAML("start")), 0644); err != nil {
		t.Fatal(err)
	}
	store := newTestStateStore(t)
	callbacks := NewWorkerCallbackManager()
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		&fakeWorkerBackend{callbacks: callbacks, scrollYAML: cachedScrollYAML("start")},
	)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	runtimeScroll, err := supervisor.Create(artifact, "quiet-create", nil)
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

func TestRuntimeSupervisorCreateUsesPullWorkerBeforeStateMutation(t *testing.T) {
	store := newTestStateStore(t)
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, scrollYAML: cachedScrollYAML("start"), digest: "sha256:worker"}
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		backend,
	)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	runtimeScroll, err := supervisor.Create("registry.local/lab:1.0", "worker-scroll", nil)
	if err != nil {
		t.Fatal(err)
	}
	if backend.action.Mode != ports.RuntimeWorkerModeCreate || backend.action.RuntimeID != "worker-scroll" {
		t.Fatalf("worker action = %#v", backend.action)
	}
	if backend.action.RootRef != backend.RootRef("worker-scroll", "") || backend.action.MountPath != "/scroll" {
		t.Fatalf("worker root = %#v, want %s mounted at /scroll", backend.action, backend.RootRef("worker-scroll", ""))
	}
	if backend.action.CallbackToken == "" || !strings.Contains(backend.action.CallbackURL, "/internal/v1/workers/worker-scroll/complete") {
		t.Fatalf("callback action = %#v", backend.action)
	}
	if runtimeScroll.ArtifactDigest != "sha256:worker" {
		t.Fatalf("artifact digest = %s, want sha256:worker", runtimeScroll.ArtifactDigest)
	}
	if runtimeScroll.Root != backend.RootRef("worker-scroll", "") {
		t.Fatalf("root = %s, want %s", runtimeScroll.Root, backend.RootRef("worker-scroll", ""))
	}
}

func TestRuntimeSupervisorCreateUsesRequestedNamespaceForRoot(t *testing.T) {
	store := newTestStateStore(t)
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, scrollYAML: cachedScrollYAML("start")}
	supervisor := NewRuntimeSupervisor(store, coreservices.NewRuntimeScrollManager(store), backend)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	runtimeScroll, err := supervisor.CreateWithOwner("registry.local/lab:1.0", "worker-scroll", "owner-a", "games", nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := backend.RootRef("worker-scroll", "games"); backend.action.RootRef != want || runtimeScroll.Root != want {
		t.Fatalf("root action=%s scroll=%s want %s", backend.action.RootRef, runtimeScroll.Root, want)
	}
}

func TestRuntimeSupervisorEnsureMaterializationFailureIsRemembered(t *testing.T) {
	store := newTestStateStore(t)
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, workerErr: errors.New("pull image failed")}
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		backend,
	)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	if _, err := supervisor.Ensure("registry.local/missing:1.0", "broken-scroll", nil); err == nil {
		t.Fatal("Ensure error = nil, want materialization error")
	}
	failed, err := store.GetScroll("broken-scroll")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != domain.RuntimeScrollStatusError || !strings.Contains(failed.LastError, "pull image failed") {
		t.Fatalf("failed scroll = %#v", failed)
	}

	runtimeScroll, err := supervisor.Ensure("registry.local/missing:1.0", "broken-scroll", nil)
	if err != nil {
		t.Fatalf("second Ensure error = %v, want remembered runtime scroll", err)
	}
	if runtimeScroll.Status != domain.RuntimeScrollStatusError || backend.spawnCount != 1 {
		t.Fatalf("runtimeScroll=%#v spawnCount=%d, want remembered error and no respawn", runtimeScroll, backend.spawnCount)
	}
}

func TestRuntimeSupervisorEnsureRepairsIncompletePlaceholder(t *testing.T) {
	store := newTestStateStore(t)
	if err := store.CreateScroll(&domain.RuntimeScroll{
		ID:       "repair-scroll",
		Artifact: "registry.local/lab:1.0",
		Root:     store.Root("repair-scroll"),
		Status:   domain.RuntimeScrollStatusCreated,
		Commands: map[string]domain.LockStatus{},
	}); err != nil {
		t.Fatal(err)
	}
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, scrollYAML: cachedScrollYAML("start"), digest: "sha256:repair"}
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		backend,
	)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	runtimeScroll, err := supervisor.Ensure("registry.local/lab:1.0", "repair-scroll", nil)
	if err != nil {
		t.Fatal(err)
	}
	if backend.spawnCount != 1 || backend.action.Mode != ports.RuntimeWorkerModeCreate {
		t.Fatalf("worker action = %#v spawnCount=%d", backend.action, backend.spawnCount)
	}
	if runtimeScroll.ScrollYAML == "" || runtimeScroll.ArtifactDigest != "sha256:repair" || runtimeScroll.Status != domain.RuntimeScrollStatusCreated {
		t.Fatalf("runtime scroll = %#v", runtimeScroll)
	}
}

func TestRuntimeSupervisorEnsureDoesNotRetryExistingError(t *testing.T) {
	store := newTestStateStore(t)
	existing := &domain.RuntimeScroll{
		ID:         "invalid-scroll",
		Artifact:   "registry.local/invalid:1.0",
		Root:       store.Root("invalid-scroll"),
		ScrollName: "invalid-scroll",
		ScrollYAML: cachedScrollYAML("start"),
		Status:     domain.RuntimeScrollStatusError,
		LastError:  "procedure field mode is unsupported",
		Commands:   map[string]domain.LockStatus{},
	}
	if err := store.CreateScroll(existing); err != nil {
		t.Fatal(err)
	}
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, scrollYAML: updatedScrollYAML("invalid-scroll")}
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		backend,
	)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	runtimeScroll, err := supervisor.Ensure(existing.Artifact, existing.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeScroll.Status != domain.RuntimeScrollStatusError || backend.spawnCount != 0 {
		t.Fatalf("runtimeScroll=%#v spawnCount=%d, want existing error and no worker", runtimeScroll, backend.spawnCount)
	}
}

func TestRuntimeSupervisorEnsureUpdatesChangedArtifact(t *testing.T) {
	store := newTestStateStore(t)
	root := "k8s://druid/druid-update-scroll-data"
	existing := &domain.RuntimeScroll{
		ID:         "update-scroll",
		Artifact:   "registry.local/lab:1.0",
		Root:       root,
		ScrollName: "old-scroll",
		ScrollYAML: cachedScrollYAML("start"),
		Status:     domain.RuntimeScrollStatusRunning,
		Commands: map[string]domain.LockStatus{
			"start": {Status: domain.ScrollLockStatusDone},
		},
		Routing: []domain.RuntimeRouteAssignment{{Name: "old-http", Host: "old.example.test"}},
	}
	if err := store.CreateScroll(existing); err != nil {
		t.Fatal(err)
	}
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, scrollYAML: updatedScrollYAML("updated-scroll")}
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		backend,
	)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	updated, err := supervisor.Ensure("registry.local/lab:2.0", "update-scroll", []domain.RegistryCredential{{Host: "registry.local", Username: "bot"}})
	if err != nil {
		t.Fatal(err)
	}

	if backend.stopRoot != root {
		t.Fatalf("stop root = %s, want %s", backend.stopRoot, root)
	}
	if backend.action.Mode != ports.RuntimeWorkerModeUpdate || backend.action.Artifact != "registry.local/lab:2.0" || backend.action.RootRef != root {
		t.Fatalf("worker action = %#v", backend.action)
	}
	if updated.Artifact != "registry.local/lab:2.0" || updated.ScrollName != "updated-scroll" {
		t.Fatalf("updated scroll = %#v", updated)
	}
	if updated.Status != domain.RuntimeScrollStatusStopped {
		t.Fatalf("status = %s, want stopped", updated.Status)
	}
	if len(updated.Commands) != 0 {
		t.Fatalf("commands = %#v, want cleared", updated.Commands)
	}
	if len(updated.Routing) != 0 {
		t.Fatalf("routing = %#v, want cleared", updated.Routing)
	}
	if !strings.Contains(updated.ScrollYAML, "updated-scroll") {
		t.Fatalf("scroll yaml = %q", updated.ScrollYAML)
	}
}

func TestRuntimeSupervisorUpdateUsesPullWorkerWhenAvailable(t *testing.T) {
	store := newTestStateStore(t)
	root := "k8s://druid/druid-update-worker-data"
	existing := &domain.RuntimeScroll{
		ID:         "update-worker",
		Artifact:   "registry.local/lab:1.0",
		Root:       root,
		ScrollName: "old-scroll",
		ScrollYAML: cachedScrollYAML("start"),
		Status:     domain.RuntimeScrollStatusStopped,
		Commands:   map[string]domain.LockStatus{},
	}
	if err := store.CreateScroll(existing); err != nil {
		t.Fatal(err)
	}
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, scrollYAML: updatedScrollYAML("updated-worker"), digest: "sha256:updated"}
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		backend,
	)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	updated, err := supervisor.Ensure("registry.local/lab:2.0", "update-worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	if backend.action.Mode != ports.RuntimeWorkerModeUpdate || backend.action.RootRef != root {
		t.Fatalf("worker action = %#v", backend.action)
	}
	if updated.Artifact != "registry.local/lab:2.0" || updated.ArtifactDigest != "sha256:updated" || updated.ScrollName != "updated-worker" {
		t.Fatalf("updated scroll = %#v", updated)
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

func TestRuntimeSessionProceduresUsesLauncherStatus(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, cachedScrollYAML(""))
	session.queueManager.RememberDoneItem("start")
	session.procedures = fakeProcedureStatuses{statuses: map[string]domain.ScrollLockStatus{
		"start.0": domain.ScrollLockStatusRunning,
	}}

	got := session.Procedures()
	if got["start.0"] != domain.ScrollLockStatusRunning {
		t.Fatalf("procedures = %#v", got)
	}
	if _, ok := got["start"]; ok {
		t.Fatalf("procedures leaked queue status: %#v", got)
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

func TestDeleteDoesNotParseScrollYAML(t *testing.T) {
	store := newTestStateStore(t)
	backend := &fakeWorkerBackend{}
	supervisor := NewRuntimeSupervisor(store, coreservices.NewRuntimeScrollManager(store), backend)
	if err := store.CreateScroll(&domain.RuntimeScroll{
		ID:         "legacy",
		Root:       "runtime://legacy",
		ScrollName: "legacy",
		ScrollYAML: "name: legacy\ncommands:\n  start:\n    procedures:\n      - mode: container\n",
		Status:     domain.RuntimeScrollStatusCreated,
	}); err != nil {
		t.Fatal(err)
	}

	if err := supervisor.DeleteWithPolicy("legacy", false); err != nil {
		t.Fatal(err)
	}
	if backend.deleteRoot != "runtime://legacy" {
		t.Fatalf("delete root = %q, want runtime://legacy", backend.deleteRoot)
	}
	if _, err := store.GetScroll("legacy"); !errors.Is(err, domain.ErrRuntimeScrollNotFound) {
		t.Fatalf("GetScroll after delete error = %v, want not found", err)
	}
}

func newRuntimeSessionForTest(t *testing.T, commands map[string]domain.LockStatus, scrollYAML string) *RuntimeSession {
	t.Helper()
	root := t.TempDir()
	store := newTestStateStore(t)
	runtimeScroll := &domain.RuntimeScroll{
		ID:         "cached",
		Artifact:   "local",
		Root:       root,
		ScrollName: "cached",
		ScrollYAML: scrollYAML,
		Commands:   commands,
	}
	if err := store.CreateScroll(runtimeScroll); err != nil {
		t.Fatal(err)
	}
	session, err := NewRuntimeSession(store, runtimeScroll, &fakeWorkerBackend{})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

type fakeWorkerBackend struct {
	callbacks  *WorkerCallbackManager
	scrollYAML string
	digest     string
	workerErr  error
	action     ports.RuntimeWorkerAction
	stopRoot   string
	deleteRoot string
	spawnCount int
}

func (f *fakeWorkerBackend) Name() string {
	return "fake-worker"
}

func (f *fakeWorkerBackend) RootRef(id string, namespace string) string {
	if namespace != "" {
		return "runtime://" + namespace + "/" + id
	}
	return "runtime://" + id
}

func (f *fakeWorkerBackend) ReadScrollFile(root string) ([]byte, error) {
	return []byte(f.scrollYAML), nil
}

func (f *fakeWorkerBackend) RunCommand(command ports.RuntimeCommand) (*int, error) {
	return nil, nil
}

func (f *fakeWorkerBackend) ExpectedPorts(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimePortStatus, error) {
	return nil, nil
}

func (f *fakeWorkerBackend) RoutingTargets(root string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimeRoutingTarget, error) {
	return nil, nil
}

func (f *fakeWorkerBackend) StartDev(ctx context.Context, action ports.RuntimeDevAction) error {
	return nil
}

func (f *fakeWorkerBackend) StopDev(ctx context.Context, root string) error { return nil }

func (f *fakeWorkerBackend) Attach(commandName string, data string) error {
	return nil
}

func (f *fakeWorkerBackend) Signal(commandName string, target string, signal string, root string) error {
	return nil
}

func (f *fakeWorkerBackend) StopRuntime(root string) error {
	f.stopRoot = root
	return nil
}

func (f *fakeWorkerBackend) DeleteRuntime(root string, purgeData bool) error {
	f.deleteRoot = root
	return nil
}

func (f *fakeWorkerBackend) BackupRuntime(ctx context.Context, root string, artifact string, registryCredentials []domain.RegistryCredential) error {
	return nil
}

func (f *fakeWorkerBackend) RestoreRuntime(ctx context.Context, root string, artifact string, registryCredentials []domain.RegistryCredential) error {
	return nil
}

func (f *fakeWorkerBackend) SpawnPullWorker(ctx context.Context, action ports.RuntimeWorkerAction) error {
	f.action = action
	f.spawnCount++
	if f.workerErr != nil {
		return f.workerErr
	}
	if f.callbacks == nil {
		return nil
	}
	return f.callbacks.Complete(action.RuntimeID, action.CallbackToken, ports.RuntimeWorkerResult{
		ScrollYAML:     f.scrollYAML,
		ArtifactDigest: f.digest,
	})
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

func updatedScrollYAML(name string) string {
	return `name: ` + name + `
desc: Updated scroll
version: 0.2.0
app_version: "2.0"
serve: start
commands:
  start:
    procedures:
      - image: alpine:3.20
        command: ["true"]
`
}

func assertQueued(t *testing.T, session *RuntimeSession, command string) {
	t.Helper()
	queue := session.queueManager.GetQueue()
	if queue[command] != domain.ScrollLockStatusWaiting {
		t.Fatalf("%s = %s, want waiting; queue=%#v", command, queue[command], queue)
	}
}

func newTestStateStore(t *testing.T) ports.RuntimeScrollStore {
	t.Helper()
	store, err := docker.NewStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

type fakeProcedureStatuses struct {
	statuses map[string]domain.ScrollLockStatus
}

func (f fakeProcedureStatuses) Run(string) error {
	return nil
}

func (f fakeProcedureStatuses) GetProcedureStatuses() map[string]domain.ScrollLockStatus {
	return f.statuses
}
