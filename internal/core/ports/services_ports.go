package ports

import (
	"io"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"go.uber.org/zap/zapcore"
)

type AuthorizerServiceInterface interface {
	CheckHeader(r *fiber.Ctx) (*time.Time, error)
	CheckQuery(token string) (*time.Time, error)
	GenerateQueryToken() string
}

type ScrollServiceInterface interface {
	GetCurrent() *domain.Scroll
	GetFile() *domain.File
	GetScrollConfigRawYaml() string
	GetDir() string
	ChangeLockStatus(process string, status string) error
	GetCwd() string
	GetLock() *domain.ScrollLock
}

type ProcessLauchnerInterface interface {
	Run(commandId string, processId string, changeStatus bool) error
	RunProcedure(*domain.Procedure, string, bool) (string, error)
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
	GetRunningProcess(processName string) *domain.Process
	Run(process string, command []string, dir string) error
	RunTty(process string, command []string, dir string) error
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
	RemoveConsole(consoleId string)
	AddConsole(consoleId string, consoleType string, inputMode string, console io.Reader) *domain.Console
	AddConsoleWithChannel(consoleId string, consoleType string, inputMode string, channel chan string) *domain.Console
}

type ProcessMonitorInterface interface {
	GetAllProcessesMetrics() map[string]*domain.ProcessMonitorMetrics
	GetPsTrees() map[string]*domain.ProcessTreeRoot
	AddProcess(pid int32, name string)
	RemoveProcess(name string)
}

type LogDriverInterface interface {
	Info(string, ...zapcore.Field)
	Debug(string, ...zapcore.Field)
	Warn(string, ...zapcore.Field)
	Error(string, ...zapcore.Field)
	LogRunCommand(string, string)
	LogRunProcedure(string, string, int)
	LogStdout(string, string, string)
}

type TemplateRendererInterface interface {
	RenderTemplate(templatePath string, data interface{}) (string, error)
	RenderScrollTemplateFiles(templateFiles []string, data interface{}, ouputPath string) error
}
