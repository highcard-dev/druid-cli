package ports

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
)

type SnapshotMode string

const (
	SnapshotModeSnapshot SnapshotMode = "snapshot"
	SnapshotModeRestore  SnapshotMode = "restore"
	SnapshotModeNoop     SnapshotMode = "noop"
)

type AuthorizerServiceInterface interface {
	CheckHeader(r *fiber.Ctx) (*time.Time, error)
	CheckQuery(token string) (*time.Time, error)
	GenerateQueryToken() string
}

type ScrollServiceInterface interface {
	GetCurrent() *domain.Scroll
	GetFile() *domain.File
	GetScrollConfigRawYaml() []byte
	GetDir() string
	GetCwd() string
	WriteNewScrollLock() *domain.ScrollLock
	GetLock() (*domain.ScrollLock, error)
	GetCommand(cmd string) (*domain.CommandInstructionSet, error)
	InitFiles(files ...string) error
	InitTemplateFiles(files ...string) error
	AddTemporaryCommand(cmd string, instructions *domain.CommandInstructionSet)
}

type ProcedureLauchnerInterface interface {
	LaunchPlugins() error
	RunProcedure(*domain.Procedure, string, []string) (string, *int, error)
	Run(cmd string, runCommandCb func(cmd string) error) error
	GetProcedureStatuses() map[string]domain.ScrollLockStatus
}

type PluginManagerInterface interface {
	CanRunStandaloneProcedure(mode string) bool
	GetNotifyConsoleChannel() chan *domain.StreamItem
	ParseFromScroll(pluginDefinitionMap map[string]map[string]string, config string, cwd string) error
	HasMode(mode string) bool
	RunProcedure(mode string, value string) (string, error)
}

type LogManagerInterface interface {
	GetStreams() map[string]*domain.Log
	AddLine(stream string, sc []byte)
}

type ProcessManagerInterface interface {
	GetRunningProcesses() map[string]*domain.Process
	GetRunningProcess(commandName string) *domain.Process
	Run(commandName string, command []string, dir string) (*int, error)
	RunTty(comandName string, command []string, dir string) (*int, error)
	WriteStdin(process *domain.Process, data string) error
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

type ProcessMonitorInterface interface {
	GetAllProcessesMetrics() map[string]*domain.ProcessMonitorMetrics
	GetPsTrees() map[string]*domain.ProcessTreeRoot
	AddProcess(pid int32, name string)
	RemoveProcess(name string)
}

type TemplateRendererInterface interface {
	RenderTemplate(templatePath string, data interface{}) (string, error)
	RenderScrollTemplateFiles(templateBase string, templateFiles []string, data interface{}, ouputPath string) error
}

type OciRegistryInterface interface {
	GetRepo(repoUrl string) (*remote.Repository, error)
	Pull(dir string, artifact string) error
	CanUpdateTag(descriptor v1.Descriptor, folder string, tag string) (bool, error)
	PackFolders(fs *file.Store, dirs []string, artifactType domain.ArtifactType, path string) ([]v1.Descriptor, error)
	Push(folder string, repo string, tag string, annotationInfo domain.AnnotationInfo, packMeta bool) (v1.Descriptor, error)
	PushMeta(folder string, repo string) (v1.Descriptor, error)
	CreateMetaDescriptors(fs *file.Store, dir string, artifact string) ([]v1.Descriptor, error)
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
	StartMonitoring(context.Context, []string, uint)
	GetLastActivity(port int) uint
	CheckOpen(prot int) bool
	GetPorts() []*domain.AugmentedPort
	MandatoryPortsOpen() bool
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

type RestoreSnapshotOptions struct {
}

type S3Destination struct {
	AccessKey string
	SecretKey string
	Bucket    string
	Endpoint  string
	Region    string
	Insecure  bool
}
type SnapshotOptions struct {
	CompressionLevel int
	S3Destination    *S3Destination
}

type ProgressTracker interface {
	LogTrackProgress(current int64)
	GetPercent() float64
}

type SnapshotService interface {
	Snapshot(dir string, destination string, options SnapshotOptions) error
	RestoreSnapshot(dir string, source string, options RestoreSnapshotOptions) error

	GetProgressTracker() *ProgressTracker
	GetCurrentMode() SnapshotMode
	GetCurrentProgressTracker() *ProgressTracker
}

type ColdStarterServerInterface interface {
	Start(port int, onFinish func()) error
	Close() error
}

type UiServiceInterface interface {
	GetIndex(filePath string) ([]string, error)
}

type UiDevServiceInterface interface {
	StartWatching(basePath string, paths ...string) error
	StopWatching() error
	Subscribe() chan *[]byte
	Unsubscribe(client chan *[]byte)
	GetWatchedPaths() []string
	IsWatching() bool
	SetHotReloadCommands(procs map[string]*domain.CommandInstructionSet)
	SetBuildCommands(procs map[string]*domain.CommandInstructionSet)
	Build() error
}

type NixDependencyServiceInterface interface {
	GetCommand(cmd []string, deps []string) []string
	EnsureNixInstalled() error
}
