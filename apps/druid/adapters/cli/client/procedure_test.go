package client

import (
	"context"
	"testing"

	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/spf13/cobra"
)

func TestProcedureRowsCombineConfigStatusAndConsoles(t *testing.T) {
	coldstart := "coldstart"
	file := &domain.File{Commands: map[string]*domain.CommandInstructionSet{
		"start": {
			Procedures: []*domain.Procedure{
				{Id: &coldstart},
				{},
			},
		},
	}}
	rows := procedureRows(file, map[string]domain.ScrollLockStatus{
		"start":     domain.ScrollLockStatusWaiting,
		"coldstart": domain.ScrollLockStatusRunning,
	}, map[string]domain.Console{
		"coldstart": {},
	})

	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[0] != (procedureRow{command: "start", procedure: "coldstart", status: "running", console: "yes"}) {
		t.Fatalf("row 0 = %#v", rows[0])
	}
	if rows[1] != (procedureRow{command: "start", procedure: "start.1", status: "waiting", console: "no"}) {
		t.Fatalf("row 1 = %#v", rows[1])
	}
}

func TestProcedureRowsDoNotMarkEveryProcedureRunningFromCommandStatus(t *testing.T) {
	coldstart := "coldstart"
	start := "start"
	file := &domain.File{Commands: map[string]*domain.CommandInstructionSet{
		"start": {
			Procedures: []*domain.Procedure{
				{Id: &coldstart},
				{Id: &start},
			},
		},
	}}
	rows := procedureRows(file, map[string]domain.ScrollLockStatus{
		"start": domain.ScrollLockStatusRunning,
	}, map[string]domain.Console{
		"coldstart": {},
	})

	if rows[0] != (procedureRow{command: "start", procedure: "coldstart", status: "running", console: "yes"}) {
		t.Fatalf("row 0 = %#v", rows[0])
	}
	if rows[1] != (procedureRow{command: "start", procedure: "start", status: "waiting", console: "no"}) {
		t.Fatalf("row 1 = %#v", rows[1])
	}
}

func TestCommandRunCallsDaemon(t *testing.T) {
	daemon := &fakeProcedureDaemon{}
	withClientConfig(t, Config{Daemon: func() (RuntimeDaemon, error) { return daemon, nil }})

	if err := CommandRunCommand.RunE(&cobra.Command{}, []string{"scroll-a", "start"}); err != nil {
		t.Fatal(err)
	}
	if daemon.runScroll != "scroll-a" || daemon.runCommand != "start" {
		t.Fatalf("run scroll=%q command=%q", daemon.runScroll, daemon.runCommand)
	}
}

func TestCommandRowsCombineConfigAndQueue(t *testing.T) {
	file := &domain.File{Commands: map[string]*domain.CommandInstructionSet{
		"install": {Run: domain.RunModeOnce, Procedures: []*domain.Procedure{{}}},
		"start":   {Run: domain.RunModeRestart, Procedures: []*domain.Procedure{{}, {}}},
	}}
	rows := commandRows(file, map[string]domain.ScrollLockStatus{
		"start": domain.ScrollLockStatusWaiting,
	})

	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[0] != (commandRow{command: "install", status: "-", runMode: "once", procedures: 1}) {
		t.Fatalf("row 0 = %#v", rows[0])
	}
	if rows[1] != (commandRow{command: "start", status: "waiting", runMode: "restart", procedures: 2}) {
		t.Fatalf("row 1 = %#v", rows[1])
	}
}

func TestProcedureAttachRequiresActiveConsole(t *testing.T) {
	daemon := &fakeProcedureDaemon{consoles: map[string]domain.Console{"start": {}}}
	var attachedScroll, attachedConsole string
	withClientConfig(t, Config{
		Daemon: func() (RuntimeDaemon, error) { return daemon, nil },
		AttachConsole: func(ctx context.Context, scroll string, console string) error {
			attachedScroll = scroll
			attachedConsole = console
			return nil
		},
	})

	if err := ProcedureAttachCommand.RunE(&cobra.Command{}, []string{"scroll-a", "start"}); err != nil {
		t.Fatal(err)
	}
	if attachedScroll != "scroll-a" || attachedConsole != "start" {
		t.Fatalf("attached scroll=%q console=%q", attachedScroll, attachedConsole)
	}
	if err := ProcedureAttachCommand.RunE(&cobra.Command{}, []string{"scroll-a", "missing"}); err == nil {
		t.Fatal("missing console should fail")
	}
}

func withClientConfig(t *testing.T, cfg Config) {
	t.Helper()
	old := config
	config = cfg
	t.Cleanup(func() { config = old })
}

type fakeProcedureDaemon struct {
	runScroll  string
	runCommand string
	consoles   map[string]domain.Console
}

func (f *fakeProcedureDaemon) CreateScroll(ctx context.Context, name string, artifact string, registryCredentials []api.RegistryCredential) (*api.RuntimeScroll, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) UpdateScroll(ctx context.Context, id string, artifact string, registryCredentials []api.RegistryCredential) (*api.RuntimeScroll, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) ListScrolls(ctx context.Context) ([]api.RuntimeScroll, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) GetScroll(ctx context.Context, id string) (*api.RuntimeScroll, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) DeleteScroll(ctx context.Context, id string) (*api.DeletedScroll, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) RunScrollCommand(ctx context.Context, id string, command string) (*api.RuntimeScroll, error) {
	f.runScroll = id
	f.runCommand = command
	return &api.RuntimeScroll{Id: id}, nil
}

func (f *fakeProcedureDaemon) GetScrollConfig(ctx context.Context, id string) (*domain.File, error) {
	return &domain.File{}, nil
}

func (f *fakeProcedureDaemon) GetScrollProcedures(ctx context.Context, id string) (map[string]domain.ScrollLockStatus, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) GetScrollQueue(ctx context.Context, id string) (map[string]domain.ScrollLockStatus, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) GetScrollConsoles(ctx context.Context, id string) (map[string]domain.Console, error) {
	return f.consoles, nil
}

func (f *fakeProcedureDaemon) GetScrollPorts(ctx context.Context, id string) ([]api.RuntimePortStatus, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) StartScroll(ctx context.Context, id string) (*api.RuntimeScroll, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) StopScroll(ctx context.Context, id string) (*api.RuntimeScroll, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) GetScrollRoutingTargets(ctx context.Context, id string) ([]api.RuntimeRoutingTarget, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) ApplyScrollRouting(ctx context.Context, id string, assignments []api.RuntimeRouteAssignment) (*api.RuntimeScroll, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) GetScrollUIPackages(ctx context.Context, id string) (map[string]api.RuntimeUIPackage, error) {
	return map[string]api.RuntimeUIPackage{}, nil
}

func (f *fakeProcedureDaemon) PublishScrollUIPackage(ctx context.Context, id string, scope string, path string) (*api.RuntimeScroll, error) {
	return &api.RuntimeScroll{Id: id}, nil
}

func (f *fakeProcedureDaemon) EnableWatch(ctx context.Context, id string, request api.DevWatchRequest) (*api.DevWatchResponse, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) DisableWatch(ctx context.Context, id string) (*api.DevWatchResponse, error) {
	return nil, nil
}

func (f *fakeProcedureDaemon) WatchStatus(ctx context.Context, id string) (*api.DevWatchStatus, error) {
	return nil, nil
}
