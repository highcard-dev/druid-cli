package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func NewDevelopmentEncoder(level zapcore.Level) zapcore.Core {

	errEncoder := zap.NewDevelopmentEncoderConfig()
	errEncoder.EncodeLevel = func(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString("[" + l.CapitalString() + "]")
	}
	errEncoder.ConsoleSeparator = " "
	errEncoder.LevelKey = "severity"
	errEncoder.MessageKey = "message"
	errEncoder.EncodeTime = zapcore.RFC3339TimeEncoder

	return zapcore.NewCore(zapcore.NewConsoleEncoder(errEncoder), zapcore.AddSync(os.Stderr), level)
}
