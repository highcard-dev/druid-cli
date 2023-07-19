package ports

import (
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
	Run(commandId string, processId string, changeStatus bool) error
	RunProcedure(*domain.Procedure, string, bool) (string, error)
	GetCurrent() *domain.Scroll
	GetFile() *domain.File
	GetRunningProcesses() map[string]*domain.Process
}

type PluginManagerInterface interface {
	CanRunStandaloneProcedure(mode string) bool
}

type LogManagerInterface interface {
	GetStreams() map[string]*domain.Log
	AddLine(stream string, sc domain.StreamCommand)
}

type ProcessManagerInterface interface {
	GetRunningProcesses() map[string]*domain.Process
	Launch(process *domain.Process, command []string, dir string) error
	WriteStdin(process *domain.Process, data string) error
}

type ProcessMonitorInterface interface {
	GetAllProcessesMetrics() map[string]*domain.ProcessMonitorMetrics
	GetPsTrees() map[string]*domain.ProcessTreeRoot
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
