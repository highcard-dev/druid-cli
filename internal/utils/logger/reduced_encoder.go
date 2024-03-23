package logger

import (
	"github.com/mattn/go-colorable"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

type ReducedEncoder struct {
	zapcore.Encoder
	pool buffer.Pool
}

func (r ReducedEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	line := r.pool.Get()

	line.AppendString(ent.Message)
	line.AppendString("\n")

	return line, nil
}

func NewReducedEncoder(level zapcore.Level) zapcore.Core {

	reducedEncoder := &ReducedEncoder{
		pool: buffer.NewPool(),
		Encoder: zapcore.NewConsoleEncoder(zapcore.EncoderConfig{
			MessageKey: "message",
		}),
	}

	return zapcore.NewCore(reducedEncoder, zapcore.AddSync(colorable.NewColorableStdout()), level)
}
func (e *ReducedEncoder) Clone() zapcore.Encoder {

	reducedEncoder := &ReducedEncoder{
		pool: buffer.NewPool(),
		Encoder: zapcore.NewConsoleEncoder(zapcore.EncoderConfig{
			MessageKey: "message",
		}),
	}
	return reducedEncoder
}
