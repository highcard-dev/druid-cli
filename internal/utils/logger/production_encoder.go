package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func NewProductionEncoder(level zapcore.Level) zapcore.Core {

	structuredEncoder := zap.NewProductionEncoderConfig()
	structuredEncoder.EncodeLevel = func(level zapcore.Level, encoder zapcore.PrimitiveArrayEncoder) {
		encoder.AppendString(level.CapitalString())
	}
	structuredEncoder.LevelKey = "severity"
	structuredEncoder.MessageKey = "message"
	structuredEncoder.EncodeTime = zapcore.ISO8601TimeEncoder
	return zapcore.NewCore(zapcore.NewJSONEncoder(structuredEncoder), zapcore.AddSync(os.Stderr), level)
}
