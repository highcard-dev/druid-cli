//taken from: https://github.com/serverless/event-gateway/blob/92b773f27dc856fa3db08c772963cb8c64f1ce89/plugin/logger.go (modified)
package logger

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"

	hclog "github.com/hashicorp/go-hclog"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Level = hclog.Level

// Hclog2ZapLogger implements Hashicorp's hclog.Logger interface using Uber's zap.Logger. It's a workaround for plugin
// system. go-plugin doesn't support other logger than hclog. This logger implements only methods used by the go-plugin.
type Hclog2ZapLogger struct {
	Zap *zap.Logger
}

// Trace implementation.
func (l Hclog2ZapLogger) Trace(msg string, args ...interface{}) {}

// Debug implementation.
func (l Hclog2ZapLogger) Debug(msg string, args ...interface{}) {
	l.Zap.Debug(msg, argsToFields(args...)...)
}

// Info implementation.
func (l Hclog2ZapLogger) Info(msg string, args ...interface{}) {
	l.Zap.Info(msg, argsToFields(args...)...)
}

// Log logs messages with four simplified levels - Debug,Warn,Error and Info as a default.
func (l Hclog2ZapLogger) Log(lvl Level, msg string, args ...interface{}) {
	switch lvl {
	case hclog.Debug:
		l.Debug(msg, args...)
	case hclog.Warn:
		l.Warn(msg, args...)
	case hclog.Error:
		l.Error(msg, args...)
	case hclog.DefaultLevel, hclog.Info, hclog.NoLevel, hclog.Off, hclog.Trace:
		l.Info(msg, args...)
	}
}

// Info implementation.
func (l Hclog2ZapLogger) Name() string {
	return "hclog2zap"
}

// Warn implementation.
func (l Hclog2ZapLogger) Warn(msg string, args ...interface{}) {
	l.Zap.Warn(msg, argsToFields(args...)...)
}

// Error implementation.
func (l Hclog2ZapLogger) Error(msg string, args ...interface{}) {
	l.Zap.Error(msg, argsToFields(args...)...)
}

// IsTrace implementation.
func (l Hclog2ZapLogger) IsTrace() bool { return false }

// IsDebug implementation.
func (l Hclog2ZapLogger) IsDebug() bool { return false }

// IsInfo implementation.
func (l Hclog2ZapLogger) IsInfo() bool { return false }

// IsWarn implementation.
func (l Hclog2ZapLogger) IsWarn() bool { return false }

// IsError implementation.
func (l Hclog2ZapLogger) IsError() bool { return false }

// GetLevel implementation.
func (l Hclog2ZapLogger) GetLevel() hclog.Level { return hclog.Level(0) }

// GetLevel implementation.
func (l Hclog2ZapLogger) ImpliedArgs() []interface{} { return nil }

// StandardWriter returns os.Stderr as io.Writer.
func (l Hclog2ZapLogger) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	return hclog.DefaultOutput
}

// With implementation.
func (l Hclog2ZapLogger) With(args ...interface{}) hclog.Logger {
	return Hclog2ZapLogger{Zap: l.Zap.With(argsToFields(args...)...)}
}

// Named implementation.
func (l Hclog2ZapLogger) Named(name string) hclog.Logger {
	return Hclog2ZapLogger{Zap: l.Zap.Named(name)}
}

// ResetNamed implementation.
func (l Hclog2ZapLogger) ResetNamed(name string) hclog.Logger {
	// no need to implement that as go-plugin doesn't use this method.
	return Hclog2ZapLogger{}
}

// SetLevel implementation.
func (l Hclog2ZapLogger) SetLevel(level hclog.Level) {
	// no need to implement that as go-plugin doesn't use this method.
}

// StandardLogger implementation.
func (l Hclog2ZapLogger) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger {
	// no need to implement that as go-plugin doesn't use this method.
	return log.New(ioutil.Discard, "", 0)
}

func argsToFields(args ...interface{}) []zapcore.Field {
	fields := []zapcore.Field{}
	for i := 0; i < len(args); i += 2 {
		fields = append(fields, zap.String(args[i].(string), fmt.Sprintf("%v", args[i+1])))
	}

	return fields
}
