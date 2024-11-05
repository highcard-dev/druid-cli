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
}

type ProcedureLauchnerInterface interface {
	LaunchPlugins() error
	RunProcedure(*domain.Procedure, string) (string, *int, error)
	Run(cmd string, runCommandCb func(cmd string) error) error
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
	RenderScrollTemplateFiles(templateFiles []string, data interface{}, ouputPath string) error
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
	GetQueue() map[string]domain.ScrollLockStatus
}

type PortServiceInterface interface {
	StartMonitoring(context.Context, []string)
	GetLastActivity(port int) uint
	CheckOpen(prot int) bool
	GetPorts() []*domain.AugmentedPort
	MandatoryPortsOpen() bool
}

type ColdStarterHandlerInterface interface {
	GetHandler(funcs map[string]func(data ...string)) (ColdStarterServerInterface, error)
	SetFinishedAt(finishedAt *time.Time)
}

type ColdStarterServerInterface interface {
	Handle(data []byte, funcs map[string]func(data ...string)) error
}

type ColdStarterInterface interface {
	Stop()
	Finish(*domain.AugmentedPort)
}
