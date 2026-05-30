package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
    run: restart
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

	queue := session.GetQueue()
	if len(queue) != 1 {
		t.Fatalf("queue len = %d, want 1: %#v", len(queue), queue)
	}
	if queue["start"] != domain.ScrollLockStatusRunning {
		t.Fatalf("start = %s, want running", queue["start"])
	}
}

func TestRuntimeSessionHydratePreservesProcedureStateWhenReattachingRunningRestart(t *testing.T) {
	root := t.TempDir()
	store := newTestStateStore(t)
	scrollYAML := multiProcedureScrollYAML()
	runtimeScroll := &domain.RuntimeScroll{
		ID:         "cached",
		Artifact:   "local",
		Root:       root,
		ScrollName: "cached",
		ScrollYAML: scrollYAML,
		Status:     domain.RuntimeScrollStatusRunning,
		Procedures: domain.ProcedureStatusMap{
			"start": {
				"coldstart": {Status: domain.ScrollLockStatusDone},
				"start":     {Status: domain.ScrollLockStatusRunning},
			},
		},
	}
	if err := store.CreateScroll(runtimeScroll); err != nil {
		t.Fatal(err)
	}
	called := make(chan ports.RuntimeCommand, 1)
	release := make(chan struct{})
	session, err := NewRuntimeSession(store, runtimeScroll, &fakeWorkerBackend{
		runCommand: func(command ports.RuntimeCommand) (*int, error) {
			called <- command
			<-release
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Hydrate(); err != nil {
		t.Fatal(err)
	}

	session.Start()
	select {
	case command := <-called:
		if command.Name != "start" {
			t.Fatalf("reattached command = %s, want start", command.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hydrated command reattach")
	}

	updated, err := store.GetScroll(runtimeScroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Procedures["start"]["coldstart"].Status != domain.ScrollLockStatusDone {
		t.Fatalf("coldstart = %s, want done; procedures=%#v", updated.Procedures["start"]["coldstart"].Status, updated.Procedures)
	}
	if updated.Procedures["start"]["start"].Status != domain.ScrollLockStatusRunning {
		t.Fatalf("start = %s, want running; procedures=%#v", updated.Procedures["start"]["start"].Status, updated.Procedures)
	}
	close(release)
	session.stopDeploymentQueue()
}

func TestRuntimeSessionHydrateSkipsMissingServe(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, cachedScrollYAML(""))

	if err := session.Hydrate(); err != nil {
		t.Fatal(err)
	}

	if queue := session.GetQueue(); len(queue) != 0 {
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
	if _, ok := updated.Procedures["missing"]; ok {
		t.Fatalf("stale command was not removed: %#v", updated.Procedures)
	}
}

func TestRuntimeSessionHydrateDoesNotRequeueRunningPersistentServe(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{
		"start": {Status: domain.ScrollLockStatusRunning},
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

	if queue := session.GetQueue(); len(queue) != 0 {
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

func TestRuntimeSessionAutoStartServeRemembersDoneOnceDependencies(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{
		"install": {Status: domain.ScrollLockStatusDone},
	}, installThenStartScrollYAML())

	if err := session.AutoStartServe(); err != nil {
		t.Fatal(err)
	}

	queue := session.GetQueue()
	if queue["install"] != domain.ScrollLockStatusDone {
		t.Fatalf("install = %s, want done; queue=%#v", queue["install"], queue)
	}
	if queue["start"] != domain.ScrollLockStatusWaiting {
		t.Fatalf("start = %s, want waiting; queue=%#v", queue["start"], queue)
	}
}

func TestRuntimeSessionStopPreservesDoneOnceCommands(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{
		"install": {Status: domain.ScrollLockStatusDone},
		"start":   {Status: domain.ScrollLockStatusRunning},
		"stop":    {Status: domain.ScrollLockStatusDone},
	}, installThenStartScrollYAML())

	if err := session.StopRuntime(); err != nil {
		t.Fatal(err)
	}

	updated, err := session.store.GetScroll(session.runtimeScroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != domain.RuntimeScrollStatusStopped {
		t.Fatalf("status = %s, want stopped", updated.Status)
	}
	if len(updated.Procedures) != 1 || updated.Procedures["install"]["install.0"].Status != domain.ScrollLockStatusDone {
		t.Fatalf("procedures = %#v, want only install done", updated.Procedures)
	}
}

func TestRuntimeSessionStopQuiescesRestartQueueBeforeBackendStop(t *testing.T) {
	store := newTestStateStore(t)
	var runs atomic.Int32
	backend := &fakeWorkerBackend{
		runCommand: func(command ports.RuntimeCommand) (*int, error) {
			runs.Add(1)
			return nil, errors.New("job deleted")
		},
	}
	runtimeScroll := &domain.RuntimeScroll{
		ID:         "restart-stop",
		Artifact:   "local",
		Root:       t.TempDir(),
		ScrollName: "restart-stop",
		ScrollYAML: `name: restart-stop
desc: Restart stop test
version: 0.1.0
app_version: "1.0"
serve: start
commands:
  start:
    run: restart
    procedures:
      - image: alpine:3.20
        command: ["false"]
`,
		Procedures: domain.ProcedureStatusMap{},
		Status:     domain.RuntimeScrollStatusRunning,
	}
	if err := store.CreateScroll(runtimeScroll); err != nil {
		t.Fatal(err)
	}
	session, err := NewRuntimeSession(store, runtimeScroll, backend)
	if err != nil {
		t.Fatal(err)
	}
	session.Start()
	if err := session.AutoStartServe(); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for runs.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("start command did not run")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if err := session.StopRuntime(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := runs.Load(); got != 1 {
		t.Fatalf("restart queue ran %d times, want 1", got)
	}
}

func TestRuntimeSupervisorStartDoesNotHydrateStoppedScroll(t *testing.T) {
	store := newTestStateStore(t)
	runtimeScroll := &domain.RuntimeScroll{
		ID:         "stopped-scroll",
		Artifact:   "local",
		Root:       "runtime://stopped-scroll",
		ScrollName: "cached",
		ScrollYAML: installThenStartScrollYAML(),
		Status:     domain.RuntimeScrollStatusStopped,
		Procedures: domain.ProcedureStatusMap{
			"install": {"install.0": {Status: domain.ScrollLockStatusDone}},
		},
	}
	if err := store.CreateScroll(runtimeScroll); err != nil {
		t.Fatal(err)
	}
	supervisor := NewRuntimeSupervisor(store, coreservices.NewRuntimeScrollManager(store), &fakeWorkerBackend{})

	if err := supervisor.Start(); err != nil {
		t.Fatal(err)
	}

	if len(supervisor.sessions) != 0 {
		t.Fatalf("sessions = %#v, want none for stopped scroll", supervisor.sessions)
	}
	updated, err := store.GetScroll("stopped-scroll")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Procedures["install"]["install.0"].Status != domain.ScrollLockStatusDone {
		t.Fatalf("procedures = %#v, want install still done", updated.Procedures)
	}
}

func TestRuntimeSupervisorStartHydratesRunningScroll(t *testing.T) {
	store := newTestStateStore(t)
	runtimeScroll := &domain.RuntimeScroll{
		ID:         "running-scroll",
		Artifact:   "local",
		Root:       "runtime://running-scroll",
		ScrollName: "cached",
		ScrollYAML: installThenStartScrollYAML(),
		Status:     domain.RuntimeScrollStatusRunning,
		Procedures: domain.ProcedureStatusMap{
			"install": {"install.0": {Status: domain.ScrollLockStatusDone}},
		},
	}
	if err := store.CreateScroll(runtimeScroll); err != nil {
		t.Fatal(err)
	}
	supervisor := NewRuntimeSupervisor(store, coreservices.NewRuntimeScrollManager(store), &fakeWorkerBackend{})

	if err := supervisor.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if session := supervisor.sessions["running-scroll"]; session != nil {
			session.stopDeploymentQueue()
		}
	}()

	session := supervisor.sessions["running-scroll"]
	if session == nil {
		t.Fatal("running scroll was not hydrated")
	}
	queue := session.GetQueue()
	if queue["install"] != domain.ScrollLockStatusDone {
		t.Fatalf("install = %s, want done; queue=%#v", queue["install"], queue)
	}
	if _, ok := queue["start"]; !ok {
		t.Fatalf("start was not queued; queue=%#v", queue)
	}
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
	if len(runtimeScroll.Procedures) != 0 {
		t.Fatalf("procedures = %#v, want empty", runtimeScroll.Procedures)
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
	if len(runtimeScroll.Procedures) != 0 {
		t.Fatalf("procedures = %#v, want empty", runtimeScroll.Procedures)
	}
}

func TestRuntimeSupervisorCreateGeneratesIDWhenNameOmitted(t *testing.T) {
	store := newTestStateStore(t)
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, scrollYAML: cachedScrollYAML("start"), digest: "sha256:generated"}
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		backend,
	)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	runtimeScroll, err := supervisor.Create("registry.local/lab:1.0", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeScroll.ID == "" || runtimeScroll.ID == "cached" {
		t.Fatalf("id = %q, want generated runtime id independent from scroll.yaml name", runtimeScroll.ID)
	}
	if runtimeScroll.ScrollName != "cached" {
		t.Fatalf("scroll name = %q, want cached", runtimeScroll.ScrollName)
	}
	if backend.action.RuntimeID != runtimeScroll.ID || backend.action.RootRef != backend.RootRef(runtimeScroll.ID, "") {
		t.Fatalf("worker action = %#v scroll = %#v", backend.action, runtimeScroll)
	}
	if backend.action.Mode != ports.RuntimeWorkerModeCreate || backend.action.CallbackToken == "" {
		t.Fatalf("worker action = %#v", backend.action)
	}
	if runtimeScroll.ArtifactDigest != "sha256:generated" || runtimeScroll.Status != domain.RuntimeScrollStatusCreated {
		t.Fatalf("runtime scroll = %#v", runtimeScroll)
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

func TestRuntimeSupervisorCreateWorkerFailureLeavesGeneratedPlaceholder(t *testing.T) {
	store := newTestStateStore(t)
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, workerErr: errors.New("pull image failed")}
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		backend,
	)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	if _, err := supervisor.Create("registry.local/missing:1.0", "", nil); err == nil {
		t.Fatal("Create error = nil, want worker error")
	}
	scrolls, err := store.ListScrolls()
	if err != nil {
		t.Fatal(err)
	}
	if len(scrolls) != 1 {
		t.Fatalf("scrolls = %#v, want one failed placeholder", scrolls)
	}
	if scrolls[0].Status != domain.RuntimeScrollStatusError || !strings.Contains(scrolls[0].LastError, "pull image failed") {
		t.Fatalf("placeholder = %#v, want remembered worker failure", scrolls[0])
	}
}

func TestRuntimeSupervisorCreateRequiresWorkerCallbackConfig(t *testing.T) {
	store := newTestStateStore(t)
	supervisor := NewRuntimeSupervisor(
		store,
		coreservices.NewRuntimeScrollManager(store),
		&fakeWorkerBackend{scrollYAML: cachedScrollYAML("start")},
	)

	if _, err := supervisor.Create("registry.local/lab:1.0", "missing-callbacks", nil); err == nil || !strings.Contains(err.Error(), "daemon materialization requires --worker-callback-url and --worker-callback-listen") {
		t.Fatalf("Create error = %v, want explicit callback config error", err)
	}
	runtimeScroll, err := store.GetScroll("missing-callbacks")
	if err != nil {
		t.Fatal(err)
	}
	if runtimeScroll.Status != domain.RuntimeScrollStatusError || !strings.Contains(runtimeScroll.LastError, "--worker-callback-url") {
		t.Fatalf("runtime scroll = %#v, want callback config error", runtimeScroll)
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

func TestRuntimeSupervisorEnsureRetriesIncompleteMaterializationFailure(t *testing.T) {
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

	backend.workerErr = nil
	backend.scrollYAML = cachedScrollYAML("start")
	backend.digest = "sha256:recovered"
	runtimeScroll, err := supervisor.Ensure("registry.local/missing:1.0", "broken-scroll", nil)
	if err != nil {
		t.Fatalf("second Ensure error = %v, want recovery", err)
	}
	if runtimeScroll.Status != domain.RuntimeScrollStatusCreated || runtimeScroll.ScrollYAML == "" || runtimeScroll.ArtifactDigest != "sha256:recovered" || backend.spawnCount != 2 {
		t.Fatalf("runtimeScroll=%#v spawnCount=%d, want recovered materialization", runtimeScroll, backend.spawnCount)
	}
}

func TestRuntimeSupervisorEnsureRepairsIncompletePlaceholder(t *testing.T) {
	store := newTestStateStore(t)
	if err := store.CreateScroll(&domain.RuntimeScroll{
		ID:         "repair-scroll",
		Artifact:   "registry.local/lab:1.0",
		Root:       store.Root("repair-scroll"),
		Status:     domain.RuntimeScrollStatusCreated,
		Procedures: domain.ProcedureStatusMap{},
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
		Procedures: domain.ProcedureStatusMap{},
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
		Procedures: domain.ProcedureStatusMap{
			"start": {"start.0": {Status: domain.ScrollLockStatusDone}},
		},
		Routing: []domain.RuntimeRouteAssignment{
			{Name: "main-route", PortName: "main", Host: "old.example.test"},
			{Name: "stale-route", PortName: "removed", Host: "gone.example.test"},
		},
	}
	if err := store.CreateScroll(existing); err != nil {
		t.Fatal(err)
	}
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, scrollYAML: updatedScrollYAMLWithPorts("updated-scroll", "main")}
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
	if len(updated.Procedures) != 0 {
		t.Fatalf("procedures = %#v, want cleared", updated.Procedures)
	}
	if len(updated.Routing) != 1 || updated.Routing[0].PortName != "main" {
		t.Fatalf("routing = %#v, want matching route preserved", updated.Routing)
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
		Procedures: domain.ProcedureStatusMap{},
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

func TestRuntimeSupervisorUpdateRefreshesCurrentArtifactAndRestartsRunningScroll(t *testing.T) {
	store := newTestStateStore(t)
	root := "runtime://refresh-worker"
	existing := &domain.RuntimeScroll{
		ID:         "refresh-worker",
		Artifact:   "registry.local/lab:1.0",
		Root:       root,
		ScrollName: "old-scroll",
		ScrollYAML: cachedScrollYAML("start"),
		Status:     domain.RuntimeScrollStatusRunning,
		Procedures: domain.ProcedureStatusMap{"start": {"start.0": {Status: domain.ScrollLockStatusDone}}},
	}
	if err := store.CreateScroll(existing); err != nil {
		t.Fatal(err)
	}
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, scrollYAML: updatedScrollYAML("refreshed-worker"), digest: "sha256:refreshed"}
	supervisor := NewRuntimeSupervisor(store, coreservices.NewRuntimeScrollManager(store), backend)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	updated, err := supervisor.Update("refresh-worker", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = supervisor.Stop("refresh-worker")
	}()

	if backend.stopRoot != root {
		t.Fatalf("stop root = %s, want %s", backend.stopRoot, root)
	}
	if backend.action.Mode != ports.RuntimeWorkerModeUpdate || backend.action.Artifact != "registry.local/lab:1.0" {
		t.Fatalf("worker action = %#v", backend.action)
	}
	if updated.Status != domain.RuntimeScrollStatusRunning {
		t.Fatalf("status = %s, want running", updated.Status)
	}
	if updated.ArtifactDigest != "sha256:refreshed" || updated.ScrollName != "refreshed-worker" {
		t.Fatalf("updated scroll = %#v", updated)
	}
}

func TestRuntimeSupervisorRestoreUsesPullWorkerResult(t *testing.T) {
	store := newTestStateStore(t)
	root := "runtime://restore-worker"
	existing := &domain.RuntimeScroll{
		ID:         "restore-worker",
		Artifact:   "registry.local/lab:1.0",
		Root:       root,
		ScrollName: "old-scroll",
		ScrollYAML: cachedScrollYAML("start"),
		Status:     domain.RuntimeScrollStatusRunning,
		Procedures: domain.ProcedureStatusMap{
			"start":    {"start.0": {Status: domain.ScrollLockStatusDone}},
			"obsolete": {"obsolete.0": {Status: domain.ScrollLockStatusDone}},
		},
		Routing: []domain.RuntimeRouteAssignment{
			{Name: "main-route", PortName: "main", Host: "old.example.test"},
			{Name: "stale-route", PortName: "removed", Host: "gone.example.test"},
		},
	}
	if err := store.CreateScroll(existing); err != nil {
		t.Fatal(err)
	}
	callbacks := NewWorkerCallbackManager()
	backend := &fakeWorkerBackend{callbacks: callbacks, scrollYAML: updatedScrollYAMLWithPorts("restored-worker", "main"), digest: "sha256:restored"}
	supervisor := NewRuntimeSupervisor(store, coreservices.NewRuntimeScrollManager(store), backend)
	supervisor.SetWorkerCallbacks(callbacks, "http://druid-cli:8083")

	restored, err := supervisor.Restore("restore-worker", "registry.local/backup:1.0", false, []domain.RegistryCredential{{Host: "registry.local", Username: "bot"}})
	if err != nil {
		t.Fatal(err)
	}

	if backend.stopRoot != root {
		t.Fatalf("stop root = %s, want %s", backend.stopRoot, root)
	}
	if backend.action.Mode != ports.RuntimeWorkerModeRestore || backend.action.RootRef != root || backend.action.Artifact != "registry.local/backup:1.0" {
		t.Fatalf("worker action = %#v", backend.action)
	}
	if restored.Artifact != "registry.local/backup:1.0" || restored.ArtifactDigest != "sha256:restored" || restored.ScrollName != "restored-worker" {
		t.Fatalf("restored scroll = %#v", restored)
	}
	if restored.Status != domain.RuntimeScrollStatusStopped {
		t.Fatalf("status = %s, want stopped", restored.Status)
	}
	if _, ok := restored.Procedures["obsolete"]; ok {
		t.Fatalf("procedures = %#v, want obsolete command removed", restored.Procedures)
	}
	if len(restored.Routing) != 1 || restored.Routing[0].PortName != "main" {
		t.Fatalf("routing = %#v, want matching route preserved", restored.Routing)
	}
}

func TestNewRuntimeSessionRequiresPersistedScrollYAML(t *testing.T) {
	store := newTestStateStore(t)
	runtimeScroll := &domain.RuntimeScroll{
		ID:         "missing-yaml",
		Artifact:   "local",
		Root:       "runtime://missing-yaml",
		Status:     domain.RuntimeScrollStatusCreated,
		Procedures: domain.ProcedureStatusMap{},
	}
	if err := store.CreateScroll(runtimeScroll); err != nil {
		t.Fatal(err)
	}

	_, err := NewRuntimeSession(store, runtimeScroll, &fakeWorkerBackend{})
	if err == nil || !strings.Contains(err.Error(), "has no scroll_yaml") {
		t.Fatalf("NewRuntimeSession error = %v, want missing scroll_yaml", err)
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

func TestRuntimeSessionQueueReturnsProcedureStatuses(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, cachedScrollYAML(""))
	session.RememberDoneItem("start")
	session.runtimeScroll.Procedures = domain.ProcedureStatusMap{"start": {"start.0": {Status: domain.ScrollLockStatusRunning}}}

	got := session.Queue()
	if got["start"]["start.0"].Status != domain.ScrollLockStatusRunning {
		t.Fatalf("queue = %#v", got)
	}
	if _, ok := got["start"]["start"]; ok {
		t.Fatalf("queue leaked command status as procedure: %#v", got)
	}
}

func TestRuntimeSessionQueueReturnsExplicitProcedureStatuses(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, multiProcedureScrollYAML())
	session.runtimeScroll.Procedures = domain.ProcedureStatusMap{"start": {
		"coldstart": {Status: domain.ScrollLockStatusDone},
		"start":     {Status: domain.ScrollLockStatusRunning},
	}}

	got := session.Queue()
	if got["start"]["start"].Status != domain.ScrollLockStatusRunning {
		t.Fatalf("queue = %#v, want start running", got)
	}
	if got["start"]["coldstart"].Status != domain.ScrollLockStatusDone {
		t.Fatalf("queue = %#v, want coldstart done", got)
	}
}

func TestRuntimeSessionQueueUsesUnnamedProcedureFallback(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, multiProcedureScrollYAML())
	session.runtimeScroll.Procedures = domain.ProcedureStatusMap{"install": {"install.0": {Status: domain.ScrollLockStatusDone}}}

	got := session.Queue()
	if got["install"]["install.0"].Status != domain.ScrollLockStatusDone {
		t.Fatalf("queue = %#v, want unnamed procedure fallback", got)
	}
}

func TestRuntimeSessionQueuePreservesExistingProcedureStatus(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, multiProcedureScrollYAML())
	session.runtimeScroll.Procedures = domain.ProcedureStatusMap{"start": {
		"start":     {Status: domain.ScrollLockStatusRunning},
		"coldstart": {Status: domain.ScrollLockStatusDone},
	}}

	got := session.Queue()
	if got["start"]["coldstart"].Status != domain.ScrollLockStatusDone {
		t.Fatalf("queue = %#v, want existing procedure status preserved", got)
	}
}

func TestRuntimeSessionQueueDoesNotChangeInternalCommandQueue(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, multiProcedureScrollYAML())
	session.RememberDoneItem("install")
	session.runtimeScroll.Procedures = domain.ProcedureStatusMap{"install": {"install.0": {Status: domain.ScrollLockStatusDone}}}

	_ = session.Queue()
	queue := session.GetQueue()
	if _, ok := queue["install.0"]; ok {
		t.Fatalf("internal queue changed by procedure state: %#v", queue)
	}
	if queue["install"] != domain.ScrollLockStatusDone {
		t.Fatalf("queue = %#v, want original command key only", queue)
	}
}

func TestDeriveRuntimeScrollStatusTreatsRunningPersistentAsRunning(t *testing.T) {
	status := deriveRuntimeScrollStatus(domain.ProcedureStatusMap{
		"start": {"start.0": {Status: domain.ScrollLockStatusRunning}},
	}, map[string]*domain.CommandInstructionSet{
		"start": {Run: domain.RunModePersistent, Procedures: []*domain.Procedure{{}}},
	})

	if status != domain.RuntimeScrollStatusRunning {
		t.Fatalf("status = %s, want running", status)
	}
}

func TestDeriveRuntimeScrollStatusTreatsDoneFiniteAsStopped(t *testing.T) {
	status := deriveRuntimeScrollStatus(domain.ProcedureStatusMap{
		"report": {"report.0": {Status: domain.ScrollLockStatusDone}},
	}, map[string]*domain.CommandInstructionSet{
		"report": {Run: domain.RunModeAlways, Procedures: []*domain.Procedure{{}}},
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
		ID:         "delete-only",
		Root:       "runtime://delete-only",
		ScrollName: "delete-only",
		ScrollYAML: "name: delete-only\ncommands:\n  start:\n    procedures:\n      - mode: container\n",
		Status:     domain.RuntimeScrollStatusCreated,
	}); err != nil {
		t.Fatal(err)
	}

	if err := supervisor.DeleteWithPolicy("delete-only", false); err != nil {
		t.Fatal(err)
	}
	if backend.deleteRoot != "runtime://delete-only" {
		t.Fatalf("delete root = %q, want runtime://delete-only", backend.deleteRoot)
	}
	if _, err := store.GetScroll("delete-only"); !errors.Is(err, domain.ErrRuntimeScrollNotFound) {
		t.Fatalf("GetScroll after delete error = %v, want not found", err)
	}
}

func newRuntimeSessionForTest(t *testing.T, commands map[string]domain.LockStatus, scrollYAML string) *RuntimeSession {
	t.Helper()
	root := t.TempDir()
	store := newTestStateStore(t)
	procedures := domain.ProcedureStatusMap{}
	scroll, err := domain.NewScrollFromBytes(root, []byte(scrollYAML))
	if err != nil {
		t.Fatal(err)
	}
	for commandName, status := range commands {
		command := scroll.Commands[commandName]
		procedures[commandName] = map[string]domain.LockStatus{}
		if command == nil || len(command.Procedures) == 0 {
			procedures[commandName][commandName+".0"] = status
			continue
		}
		for idx, procedure := range command.Procedures {
			procedures[commandName][domain.ProcedureName(commandName, idx, procedure)] = status
		}
	}
	runtimeScroll := &domain.RuntimeScroll{
		ID:         "cached",
		Artifact:   "local",
		Root:       root,
		ScrollName: "cached",
		ScrollYAML: scrollYAML,
		Procedures: procedures,
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
	callbacks   *WorkerCallbackManager
	scrollYAML  string
	digest      string
	workerErr   error
	action      ports.RuntimeWorkerAction
	stopRoot    string
	deleteRoot  string
	spawnCount  int
	runCommand  func(ports.RuntimeCommand) (*int, error)
	stopRuntime func(string) error
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

func (f *fakeWorkerBackend) RunCommand(command ports.RuntimeCommand) (*int, error) {
	if f.runCommand != nil {
		return f.runCommand(command)
	}
	return nil, nil
}

func (f *fakeWorkerBackend) PublishUIPackage(ctx context.Context, action ports.RuntimeUIPackageAction) (ports.RuntimeUIPackageResult, error) {
	return ports.RuntimeUIPackageResult{URL: "http://packages/" + action.RuntimeID + "/" + string(action.Scope) + "/app.wasm", Path: action.SourcePath, SHA256: "sha256"}, nil
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
	if f.stopRuntime != nil {
		return f.stopRuntime(root)
	}
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

func installThenStartScrollYAML() string {
	return `name: cached
desc: Cached scroll
version: 0.1.0
app_version: "1.0"
serve: start
commands:
  install:
    run: once
    procedures:
      - image: alpine:3.20
        command: ["true"]
  start:
    needs:
      - install
    run: restart
    procedures:
      - image: alpine:3.20
        command: ["true"]
  stop:
    run: always
    procedures:
      - type: signal
        target: start
        signal: SIGTERM
`
}

func multiProcedureScrollYAML() string {
	return `name: cached
desc: Cached scroll
version: 0.1.0
app_version: "1.0"
commands:
  install:
    run: once
    procedures:
      - image: alpine:3.20
        command: ["true"]
  start:
    run: restart
    procedures:
      - id: coldstart
        image: artifacts.druid.gg/druid-team/druid:stable
        command: ["druid-coldstarter"]
      - id: start
        image: eclipse-temurin:21-jre
        command: ["sh", "./start.sh"]
`
}

func updatedScrollYAML(name string) string {
	return `name: ` + name + `
desc: Updated scroll
version: 0.2.0
app_version: "2.0"
serve: start
commands:
  start:
    run: restart
    procedures:
      - image: alpine:3.20
        command: ["true"]
`
}

func updatedScrollYAMLWithPorts(name string, ports ...string) string {
	yaml := `name: ` + name + `
desc: Updated scroll
version: 0.2.0
app_version: "2.0"
ports:
`
	for _, port := range ports {
		yaml += `  - name: ` + port + `
    protocol: tcp
    port: 80
`
	}
	yaml += `serve: start
commands:
  start:
    procedures:
      - image: alpine:3.20
        command: ["true"]
`
	return yaml
}

func assertQueued(t *testing.T, session *RuntimeSession, command string) {
	t.Helper()
	queue := session.GetQueue()
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
