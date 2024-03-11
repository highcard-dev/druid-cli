package ports

import (
	"io"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
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
}

type ProcessLauchnerInterface interface {
	RunNew(commandId string, processId string, changeStatus bool) error
	RunProcedure(*domain.Procedure, string, string) (string, *int, error)
}

type PluginManagerInterface interface {
	CanRunStandaloneProcedure(mode string) bool
}

type LogManagerInterface interface {
	GetStreams() map[string]*domain.Log
	AddLine(stream string, sc []byte)
}

type ProcessManagerInterface interface {
	GetRunningProcesses() map[string]*domain.Process
	GetRunningProcess(process string, commandName string) *domain.Process
	Run(process string, commandName string, command []string, dir string) (*int, error)
	RunTty(process string, comandName string, command []string, dir string) (*int, error)
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
