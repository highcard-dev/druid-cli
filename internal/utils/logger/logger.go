package logger

import (
	"sync"
	"testing"

	"github.com/highcard-dev/daemon/internal/utils/env"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	LogKeyLogType          = "log-type"
	LogKeyApplication      = "app"
	LogKeyContext          = "context"
	LogContextControlPlane = "control-plane"
	LogContextMessage      = "message-service"
	LogContextUpdateScroll = "update-scroll"
	LogContextSwitchScroll = "switch-scroll"
	LogContextDataPlane    = "data-plane"
	LogContextMain         = "main"
	LogContextSignal       = "signal"
	LogContextHttp         = "http"
	LogContextGrpc         = "grpc"
	LogContextScroll       = "scroll"
	LogContextProcedure    = "scroll-procedure"
	LogContextMonitor      = "monitor"
	LogContextProcess      = "scroll-process"
	LogContextWebSocket    = "web-socket"
	LogContextUpdate       = "update"
	LogContextView         = "view"
	LogContextPush         = "push"
)

var (
	logOnce        sync.Once
	logger         *zap.Logger
	testingContext bool
)

type LoggerOptions struct {
	WithStructureLogging bool
	WithReducedLogging   bool
	WithDefaultLogging   bool
	LogLevel             zapcore.Level
	DefaultFields        []zap.Field
}

type LoggerOptionsFunc func(*LoggerOptions) error

func Log(optFuncs ...LoggerOptionsFunc) *zap.Logger {
	logOnce.Do(func() {
		if testingContext {
			return
		}
		logger = NewLogger(optFuncs...)
	})
	return logger
}

func SetTestLogger(t *testing.T) {
	logger = zaptest.NewLogger(t)
}

func SetupLogsCapture() *observer.ObservedLogs {
	core, logs := observer.New(zap.InfoLevel)
	logger = zap.New(core)
	testingContext = true
	return logs
}

func WithStructuredLogging() LoggerOptionsFunc {
	return func(options *LoggerOptions) error {
		options.WithStructureLogging = true
		return nil
	}
}

func WithDefaultLogging() LoggerOptionsFunc {
	return func(options *LoggerOptions) error {
		options.WithDefaultLogging = true
		return nil
	}
}

func WithReducedLogging() LoggerOptionsFunc {
	return func(options *LoggerOptions) error {
		options.WithReducedLogging = true
		return nil
	}
}

func WithDefaultFields(fields []zap.Field) LoggerOptionsFunc {
	return func(options *LoggerOptions) error {
		options.DefaultFields = fields
		return nil
	}
}

func NewLogger(optFuncs ...LoggerOptionsFunc) *zap.Logger {
	var options = &LoggerOptions{}
	var cores []zapcore.Core
	for _, optFunc := range optFuncs {
		if err := optFunc(options); err != nil {
			panic("error instantiating new logger: " + err.Error())
		}
	}

	// set log level by env first
	var loglevel zapcore.Level
	envLogLevel := env.CanGet("LOG_LEVEL")
	if envLogLevel != "" {
		if envLogLevel == "debug" {
			loglevel = zapcore.DebugLevel
		} else if envLogLevel == "info" {
			loglevel = zapcore.InfoLevel
		} else if envLogLevel == "warn" {
			loglevel = zapcore.WarnLevel
		} else if envLogLevel == "error" {
			loglevel = zapcore.ErrorLevel
		} else if envLogLevel == "panic" {
			loglevel = zapcore.PanicLevel
		} else if envLogLevel == "fatal" {
			loglevel = zapcore.FatalLevel
		} else {
			loglevel = zapcore.InfoLevel
		}
	}

	if loglevel == 0 {
		options.LogLevel = zapcore.InfoLevel
	} else {
		options.LogLevel = loglevel
	}

	if options.WithStructureLogging {
		cores = append(cores, NewProductionEncoder(options.LogLevel))
	}

	if options.WithDefaultLogging {
		cores = append(cores, NewDevelopmentEncoder(options.LogLevel))
	}
	if options.WithReducedLogging {
		cores = append(cores, NewReducedEncoder(options.LogLevel))
	}
	core := zapcore.NewTee(cores...)
	l := zap.New(core)
	if len(options.DefaultFields) > 0 {
		l = l.With(options.DefaultFields...)
	}
	return l
}
