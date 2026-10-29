package logger

import (
	"github.com/highcard-dev/daemon/internal/workloadidentity"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

// Redact after encoding so messages, errors, nested fields and inherited fields
// all receive the same protection, including application output in failures.
type credentialEncoder struct{ zapcore.Encoder }

func (e credentialEncoder) Clone() zapcore.Encoder {
	return credentialEncoder{e.Encoder.Clone()}
}

func (e credentialEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	encoded, err := e.Encoder.EncodeEntry(entry, fields)
	if err != nil {
		return encoded, err
	}
	redacted := workloadidentity.Redact(encoded.String())
	encoded.Reset()
	encoded.AppendString(redacted)
	return encoded, nil
}
