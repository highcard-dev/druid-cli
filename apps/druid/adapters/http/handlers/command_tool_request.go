package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	services "github.com/highcard-dev/daemon/apps/druid/core/services"
	"github.com/highcard-dev/daemon/internal/workloadidentity"
)

func commandToolStatus(err error) int {
	var failure *services.ToolError
	if errors.As(err, &failure) {
		return failure.Status
	}
	if errors.Is(err, workloadidentity.ErrUnauthorized) {
		return 401
	}
	if errors.Is(err, workloadidentity.ErrUnavailable) {
		return 503
	}
	return 500
}

func commandToolMessage(err error) string {
	if commandToolStatus(err) == 500 {
		return "runtime tool failed"
	}
	return workloadidentity.Redact(err.Error())
}

func bearer(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func commandToolInput(data []byte) (services.ToolInput, error) {
	var input services.ToolInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&input)
	return input, err
}
