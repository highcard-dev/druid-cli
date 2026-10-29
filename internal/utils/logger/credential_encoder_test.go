package logger

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/workloadidentity"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestCredentialRedactionIncludesInheritedAndNestedLogFields(t *testing.T) {
	token, err := workloadidentity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	log := zap.New(NewProductionEncoderForWriter(zapcore.InfoLevel, zapcore.AddSync(&output))).With(zap.String("inherited", token))
	log.Error("failure "+token, zap.Error(errors.New(token)), zap.Any("nested", map[string]string{"token": token}))
	if strings.Contains(output.String(), token) || strings.Count(output.String(), "[REDACTED]") != 4 {
		t.Fatal("issued credentials leaked through a log field")
	}
}
