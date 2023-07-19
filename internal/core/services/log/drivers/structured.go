package log_drivers

import (
	"github.com/highcard-dev/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type StructuredLogDriver struct {
	zapLogger *zap.Logger
}

func NewStructuredLogDriver() *StructuredLogDriver {
	return &StructuredLogDriver{
		zapLogger: logger.Log(logger.WithDefaultLogging()),
	}
}

func (s *StructuredLogDriver) Info(msg string, fields ...zapcore.Field) {
	s.zapLogger.Info(msg, fields...)
}

func (s *StructuredLogDriver) Debug(msg string, fields ...zapcore.Field) {
	s.zapLogger.Debug(msg, fields...)
}

func (s *StructuredLogDriver) Error(msg string, fields ...zapcore.Field) {
	s.zapLogger.Error(msg, fields...)
}

func (s *StructuredLogDriver) Warn(msg string, fields ...zapcore.Field) {
	s.zapLogger.Warn(msg, fields...)
}

func (s *StructuredLogDriver) LogRunCommand(processId string, cmd string) {
	s.zapLogger.Info("Running command", zap.String("processId", processId), zap.String("cmd", cmd))
}

func (s *StructuredLogDriver) LogRunProcedure(processId string, cmd string, i int) {
	s.zapLogger.Info("Running procedure", zap.String("processId", processId), zap.String("cmd", cmd), zap.Int("i", i))
}

func (s *StructuredLogDriver) LogStdout(process string, cmd string, data string) {
	s.zapLogger.Info(data, zap.String("command", cmd))
}
