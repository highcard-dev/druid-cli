package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	appservices "github.com/highcard-dev/daemon/apps/druid/core/services"
)

func TestRuntimeCallbackHandlerReportsProgress(t *testing.T) {
	callbacks := appservices.NewWorkerCallbackManager()
	token, _, err := callbacks.Register("runtime-1")
	if err != nil {
		t.Fatal(err)
	}
	handler := runtimeCallbackHandler{callbacks: callbacks}
	app := fiber.New()
	app.Post("/internal/v1/workers/:runtime_id/progress", handler.ReportProgress)

	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/workers/runtime-1/progress",
		strings.NewReader(fmt.Sprintf(`{"token":%q,"percentage":42}`, token)),
	)
	request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d; want %d", response.StatusCode, http.StatusNoContent)
	}
	if progress, ok := callbacks.Progress("runtime-1"); !ok || progress != 42 {
		t.Fatalf("progress = %v, %v; want 42, true", progress, ok)
	}
}
