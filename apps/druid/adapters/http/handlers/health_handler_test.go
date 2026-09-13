package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
)

func TestGetHealthAuthIncludesPullProgress(t *testing.T) {
	handler := NewHealthHandlerWithProgress(func(runtimeID string) (float64, bool) {
		return 37, runtimeID == "scroll-1"
	})
	app := fiber.New()
	app.Get("/:id/api/v1/health", handler.GetHealthAuth)

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/scroll-1/api/v1/health", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var health api.HealthResponse
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.Progress == nil || *health.Progress != 37 {
		t.Fatalf("progress = %v; want 37", health.Progress)
	}
}
