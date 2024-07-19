package ports

import (
	"io"
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
	Run(cmd string) error
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
	GetSubscription(consoleId string) chan *[]byte
	DeleteSubscription(consoleId string, subscription chan *[]byte)
	GetConsoles() map[string]*domain.Console
	RemoveConsole(consoleId string) error
	AddConsoleWithIoReader(consoleId string, consoleType domain.ConsoleType, inputMode string, console io.Reader) *domain.Console
	AddConsoleWithChannel(consoleId string, consoleType domain.ConsoleType, inputMode string, channel chan string) *domain.Console
	MarkExited(id string, exitCode int) *domain.Console
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
	PackFolders(fs *file.Store, dirs []string, artifactType domain.ArtifactType, path string) (v1.Descriptor, error)
	Push(folder string, repo string, tag string, annotationInfo domain.AnnotationInfo, packMeta bool) (v1.Descriptor, error)
	PushMeta(folder string, repo string) (v1.Descriptor, error)
	CreateMetaDescriptors(fs *file.Store, dir string, artifact string) (v1.Descriptor, error)
}

type CronManagerInterface interface {
	Init()
}

type QueueManagerInterface interface {
	AddItem(cmd string, changeStatus bool) error
}
