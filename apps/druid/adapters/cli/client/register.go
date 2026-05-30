package client

import (
	"context"
	"fmt"

	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/spf13/cobra"
)

type RuntimeDaemon interface {
	CreateScroll(ctx context.Context, name string, artifact string, registryCredentials []api.RegistryCredential) (*api.RuntimeScroll, error)
	UpdateScroll(ctx context.Context, id string, artifact string, registryCredentials []api.RegistryCredential) (*api.RuntimeScroll, error)
	ListScrolls(ctx context.Context) ([]api.RuntimeScroll, error)
	GetScroll(ctx context.Context, id string) (*api.RuntimeScroll, error)
	DeleteScroll(ctx context.Context, id string) (*api.DeletedScroll, error)
	GetScrollConfig(ctx context.Context, id string) (*domain.File, error)
	GetScrollQueue(ctx context.Context, id string) (domain.ProcedureStatusMap, error)
	GetScrollConsoles(ctx context.Context, id string) (map[string]domain.Console, error)
	GetScrollPorts(ctx context.Context, id string) ([]api.RuntimePortStatus, error)
	StartScroll(ctx context.Context, id string) (*api.RuntimeScroll, error)
	StopScroll(ctx context.Context, id string) (*api.RuntimeScroll, error)
	GetScrollRoutingTargets(ctx context.Context, id string) ([]api.RuntimeRoutingTarget, error)
	ApplyScrollRouting(ctx context.Context, id string, assignments []api.RuntimeRouteAssignment) (*api.RuntimeScroll, error)
	GetScrollUIPackages(ctx context.Context, id string) (map[string]api.RuntimeUIPackage, error)
	PublishScrollUIPackage(ctx context.Context, id string, scope string, path string) (*api.RuntimeScroll, error)
	EnableWatch(ctx context.Context, id string, request api.DevWatchRequest) (*api.DevWatchResponse, error)
	DisableWatch(ctx context.Context, id string) (*api.DevWatchResponse, error)
	WatchStatus(ctx context.Context, id string) (*api.DevWatchStatus, error)
}

type Config struct {
	Daemon              func() (RuntimeDaemon, error)
	AttachConsole       func(ctx context.Context, scroll string, console string) error
	RegistryCredentials func() []api.RegistryCredential
}

var config Config

func Register(root *cobra.Command, cfg Config) {
	config = cfg
	RoutingCommand.AddCommand(RoutingTargetsCommand, RoutingApplyCommand)
	ProcedureCommand.AddCommand(ProcedureListCommand, ProcedureAttachCommand)
	root.AddCommand(
		CreateCommand,
		DeleteCommand,
		DescribeCommand,
		DevCommand,
		ListCommand,
		PortsCommand,
		ProcedureCommand,
		StartCommand,
		StopCommand,
		RoutingCommand,
		UpdateCommand,
	)
}

func RegistryCredentials(in []domain.RegistryCredential) []api.RegistryCredential {
	out := make([]api.RegistryCredential, 0, len(in))
	for _, credential := range in {
		out = append(out, api.RegistryCredential{
			Host:     credential.Host,
			Username: credential.Username,
			Password: credential.Password,
		})
	}
	return out
}

func runtimeDaemonClient() (RuntimeDaemon, error) {
	if config.Daemon == nil {
		return nil, fmt.Errorf("client daemon is not configured")
	}
	return config.Daemon()
}

func registryCredentials() []api.RegistryCredential {
	if config.RegistryCredentials == nil {
		return nil
	}
	return config.RegistryCredentials()
}
