package docker

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

func TestContainerNameUsesDeploymentCommandAndProcedureIndex(t *testing.T) {
	root := "docker-volume://druid-0636a354-b3f4-4471-8749-3e17ab6c52-data"

	got := ContainerName(root, procedureResourceName("start", 0))
	want := "0636a354-b3f4-4471-8749-3e17ab6c52-start-0"
	if got != want {
		t.Fatalf("container name = %q, want %q", got, want)
	}
}

func TestDockerSetupErrorMarksPortConflictNonRetryable(t *testing.T) {
	for _, message := range []string{
		"Error response from daemon: ports are not available: listen tcp 0.0.0.0:25565: bind: address already in use",
		"Error response from daemon: failed to set up container networking: Bind for 127.0.0.1:25565 failed: port is already allocated",
	} {
		err := dockerSetupError(errors.New(message))
		if !domain.IsNonRetryableCommandError(err) {
			t.Fatalf("error = %v, want non-retryable", err)
		}
	}
}

func TestDockerProcedureAttemptNameKeepsFirstAttemptClean(t *testing.T) {
	base := "lolwtf-start-1"
	if got := dockerProcedureAttemptName(base, 1); got != base {
		t.Fatalf("attempt 1 name = %s, want %s", got, base)
	}
	if got := dockerProcedureAttemptName(base, 2); got != base+"-r2" {
		t.Fatalf("attempt 2 name = %s, want %s-r2", got, base)
	}
}

func TestDockerProcedureAttemptNumberUsesLabelThenName(t *testing.T) {
	if got := dockerProcedureAttemptNumber(map[string]string{dockerLabelAttempt: "3"}, "lolwtf-start-1-r2", "lolwtf-start-1"); got != 3 {
		t.Fatalf("attempt = %d, want label value 3", got)
	}
	if got := dockerProcedureAttemptNumber(nil, "lolwtf-start-1-r2", "lolwtf-start-1"); got != 2 {
		t.Fatalf("attempt = %d, want name suffix 2", got)
	}
	if got := dockerProcedureAttemptNumber(nil, "lolwtf-start-1", "lolwtf-start-1"); got != 1 {
		t.Fatalf("attempt = %d, want 1", got)
	}
}

func TestDockerAttemptHelpersSelectActiveAndPruneOldFailures(t *testing.T) {
	attempts := []dockerProcedureAttempt{
		{Name: "a1", Attempt: 1, Created: 10, ExitCode: 1},
		{Name: "a2", Attempt: 2, Created: 20, ExitCode: 1},
		{Name: "a3", Attempt: 3, Created: 30, ExitCode: 1},
		{Name: "a4", Attempt: 4, Created: 40, ExitCode: 1},
		{Name: "running", Attempt: 5, Created: 50, Running: true, Start: true},
		{Name: "success", Attempt: 6, Created: 60, ExitCode: 0},
	}
	active := activeDockerProcedureAttempt(attempts)
	if active == nil || active.Name != "running" || !active.Start {
		t.Fatalf("active = %#v", active)
	}
	pruned := prunedDockerProcedureAttempts(attempts, 3)
	if len(pruned) != 1 || pruned[0].Name != "a1" {
		t.Fatalf("pruned = %#v, want oldest failed attempt", pruned)
	}
	successes := successfulDockerProcedureAttempts(attempts)
	if len(successes) != 1 || successes[0].Name != "success" {
		t.Fatalf("successes = %#v", successes)
	}
	if got := nextDockerProcedureAttempt(attempts); got != 7 {
		t.Fatalf("next attempt = %d, want 7", got)
	}
}

func TestDockerProcedureLabelsIncludeStableIdentity(t *testing.T) {
	root := "docker-volume://druid-lolwtf-data"
	labels := dockerProcedureLabels(root, "start", "coldstart", "start-0", 2, map[string]string{"existing": "true"})
	for key, want := range map[string]string{
		"existing":           "true",
		dockerLabelRole:      dockerRoleProcedure,
		dockerLabelRuntimeID: "lolwtf",
		dockerLabelCommand:   "start",
		dockerLabelProcedure: "coldstart",
		dockerLabelResource:  "start-0",
		dockerLabelAttempt:   "2",
		dockerLabelRootHash:  rootHash(root),
	} {
		if labels[key] != want {
			t.Fatalf("label %s = %s, want %s in %#v", key, labels[key], want, labels)
		}
	}
}

func TestDockerContainerFailureMessageIncludesUsefulDetails(t *testing.T) {
	message := dockerContainerFailureMessage("lolwtf-start-1-r2", "abc123", "minecraft:test", 137, true, "killed", "last log line")
	for _, want := range []string{"lolwtf-start-1-r2", "abc123", "minecraft:test", "137", "oom_killed=true", "killed", "last log line"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message = %q, want %q", message, want)
		}
	}
}

func TestRoutingTargetsUseFirstConcreteProcedureForSharedDockerPort(t *testing.T) {
	root := "docker-volume://druid-minecraft-data"
	coldstart := "coldstart"
	start := "start"

	targets, err := (&Backend{}).RoutingTargets(root, map[string]*domain.CommandInstructionSet{
		"start": {Procedures: []*domain.Procedure{
			{Id: &coldstart, ExpectedPorts: []domain.ExpectedPort{{Name: "main"}}},
			{Id: &start, ExpectedPorts: []domain.ExpectedPort{{Name: "main"}}},
		}},
	}, []domain.Port{{Name: "main", Port: 25565, Protocol: "tcp"}})
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range targets {
		if target.Name != "main" {
			continue
		}
		if target.Procedure != "coldstart" || target.ServiceName != ContainerName(root, "start-0") {
			t.Fatalf("target = %#v", target)
		}
		return
	}
	t.Fatalf("main target missing: %#v", targets)
}

func TestContainerSpecAddsHostGatewayExtraHost(t *testing.T) {
	_, hostConfig, err := containerSpec("start", &domain.Procedure{Image: "busybox"}, "docker-volume://druid-scroll-data", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" {
		if len(hostConfig.ExtraHosts) != 0 {
			t.Fatalf("extra hosts = %#v, want none on %s", hostConfig.ExtraHosts, runtime.GOOS)
		}
		return
	}
	for _, extraHost := range hostConfig.ExtraHosts {
		if extraHost == dockerHostGatewayExtraHost {
			return
		}
	}
	t.Fatalf("extra hosts = %#v, want %s", hostConfig.ExtraHosts, dockerHostGatewayExtraHost)
}

func TestWorkerCallbackDefaultsLiveInDockerBackend(t *testing.T) {
	backend := &Backend{}
	config := backend.WorkerCallbackDefaults(ports.RuntimeWorkerCallbackConfig{})
	if config.Listen != "127.0.0.1:0" || config.URL != "" {
		t.Fatalf("config = %#v", config)
	}
	config.Listen = "127.0.0.1:12345"
	config, err := backend.WorkerCallbackAfterListen(config)
	if err != nil {
		t.Fatal(err)
	}
	if config.URL != "http://host.docker.internal:12345" {
		t.Fatalf("config = %#v", config)
	}
}

func TestWorkerCallbackDefaultsPreserveExplicitURL(t *testing.T) {
	backend := &Backend{}
	config := backend.WorkerCallbackDefaults(ports.RuntimeWorkerCallbackConfig{
		Listen: "127.0.0.1:0",
		URL:    "http://callback.example:8083",
	})
	if config.Listen != "127.0.0.1:0" || config.URL != "http://callback.example:8083" {
		t.Fatalf("config = %#v", config)
	}
	config.Listen = "127.0.0.1:12345"
	config, err := backend.WorkerCallbackAfterListen(config)
	if err != nil {
		t.Fatal(err)
	}
	if config.URL != "http://callback.example:8083" {
		t.Fatalf("config = %#v", config)
	}
}
