package ports

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
)

type AuthorizerServiceInterface interface {
	CheckHeader(r *fiber.Ctx) (*time.Time, error)
	CheckQuery(token string) (*time.Time, error)
	GenerateQueryToken() string
}

type ScrollServiceInterface interface {
	GetCurrent() *domain.Scroll
	GetFile() *domain.File
	GetDir() string
	GetCwd() string
	GetCommand(cmd string) (*domain.CommandInstructionSet, error)
}

type ProcedureLauchnerInterface interface {
	Run(cmd string) error
	GetProcedureStatuses() map[string]domain.ScrollLockStatus
}

type LogManagerInterface interface {
	GetStreams() map[string]*domain.Log
	AddLine(stream string, sc []byte)
}

type RuntimeBackendInterface interface {
	Name() string
	ReadScrollFile(scrollRoot string) ([]byte, error)
	RunCommand(command RuntimeCommand) (*int, error)
	ExpectedPorts(dataRoot string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimePortStatus, error)
	Attach(commandName string, data string) error
	Signal(commandName string, target string, signal string, dataRoot string) error
}

type RuntimeLifecycleBackendInterface interface {
	StopRuntime(dataRoot string) error
	DeleteRuntime(dataRoot string, purgeData bool) error
}

type RuntimeRoutingBackendInterface interface {
	RoutingTargets(dataRoot string, commands map[string]*domain.CommandInstructionSet, globalPorts []domain.Port) ([]domain.RuntimeRoutingTarget, error)
}

type RuntimeBackupBackendInterface interface {
	BackupRuntime(ctx context.Context, dataRoot string, artifact string) error
	RestoreRuntime(ctx context.Context, dataRoot string, artifact string) error
}

type RuntimeFileBackendInterface interface {
	ReadDataFile(ctx context.Context, dataRoot string, relativePath string) ([]byte, error)
	WriteDataFile(ctx context.Context, dataRoot string, relativePath string, data []byte) error
}

type RuntimeCommand struct {
	Name         string
	ScrollID     string
	Command      *domain.CommandInstructionSet
	DataRoot     string
	GlobalPorts  []domain.Port
	ProcedureEnv map[string]map[string]string
}

type RuntimeMaterialization struct {
	Artifact   string
	ScrollRoot string
	DataRoot   string
	ScrollYAML []byte
}

type RuntimeMaterializerInterface interface {
	MaterializeScroll(ctx context.Context, artifact string, requestedName string) (*RuntimeMaterialization, error)
}

type BroadcastChannelInterface interface {
	NewHub() *domain.BroadcastChannel
	Run()
}

type ConsoleManagerInterface interface {
	GetConsole(consoleId string) *domain.Console
	GetConsoles() map[string]*domain.Console
	AddConsoleWithChannel(consoleId string, consoleType domain.ConsoleType, inputMode string, channel chan string) (*domain.Console, chan struct{})
}

type OciRegistryInterface interface {
	GetRepo(repoUrl string) (*remote.Repository, error)
	Pull(dir string, artifact string) error
	PullSelective(dir string, artifact string, includeData bool, progress *domain.SnapshotProgress) error
	CanUpdateTag(descriptor v1.Descriptor, folder string, tag string) (bool, error)
	Push(folder string, repo string, tag string, overrides map[string]string, packMeta bool, scrollFile *domain.File) (v1.Descriptor, error)
}

type CronManagerInterface interface {
	Init()
}

type QueueManagerInterface interface {
	AddAndRememberItem(cmd string) error
	AddTempItem(cmd string) error
	AddShutdownItem(cmd string) error
	AddTempItemWithWait(cmd string) error
	GetQueue() map[string]domain.ScrollLockStatus
}

type PortServiceInterface interface {
	GetPorts() []*domain.AugmentedPort
}

type ColdStarterHandlerInterface interface {
	GetHandler(funcs map[string]func(data ...string)) (ColdStarterPacketHandlerInterface, error)
	SetFinishedAt(finishedAt *time.Time)
	Close() error
}

type ColdStarterPacketHandlerInterface interface {
	Handle(data []byte, funcs map[string]func(data ...string)) error
}

type ColdStarterInterface interface {
	Stop()
	StopWithDeplay(uint)
	Finish(*domain.AugmentedPort)
}

type ColdStarterServerInterface interface {
	Start(port int, onFinish func()) error
	Close() error
}

type UiServiceInterface interface {
	GetIndex(filePath string) ([]string, error)
}

type WatchServiceInterface interface {
	StartWatching(basePath string, paths ...string) error
	StopWatching() error
	Subscribe() chan *[]byte
	Unsubscribe(client chan *[]byte)
	GetWatchedPaths() []string
	IsWatching() bool
	SetHotReloadCommands(procs []string) error
}
