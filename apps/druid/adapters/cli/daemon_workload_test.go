package cli

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type recordingWorkloadAuthenticator struct {
	called bool
	token  string
}

func (a *recordingWorkloadAuthenticator) AuthenticateWorkload(_ context.Context, token string) (ports.RuntimeWorkloadIdentity, error) {
	a.called = true
	a.token = token
	return ports.RuntimeWorkloadIdentity{Kind: "operator", RuntimeID: "runtime-a"}, nil
}

func TestUnsafeModeStillAuthenticatesPresentedWorkloadToken(t *testing.T) {
	authenticator := &recordingWorkloadAuthenticator{}
	app := fiber.New()
	app.Use(workloadIdentityMiddleware(authenticator, true))
	app.Post("/api/v1/scrolls/runtime-a/commands/test", func(c *fiber.Ctx) error {
		identity := c.Locals("druid-workload-identity").(ports.RuntimeWorkloadIdentity)
		return c.SendString(identity.Kind)
	})

	request := httptest.NewRequest("POST", "/api/v1/scrolls/runtime-a/commands/test", nil)
	request.Header.Set("Authorization", "Bearer projected-token")
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || string(body) != "operator" || !authenticator.called || authenticator.token != "projected-token" {
		t.Fatalf("status=%d body=%q authenticator=%#v", response.StatusCode, body, authenticator)
	}
}

func TestUnsafeModeRetainsHeaderFallbackWithoutToken(t *testing.T) {
	authenticator := &recordingWorkloadAuthenticator{}
	app := fiber.New()
	app.Use(workloadIdentityMiddleware(authenticator, true))
	app.Post("/api/v1/scrolls/runtime-a/commands/test", func(c *fiber.Ctx) error {
		identity := c.Locals("druid-workload-identity").(ports.RuntimeWorkloadIdentity)
		return c.SendString(identity.Kind + ":" + identity.RuntimeID)
	})

	request := httptest.NewRequest("POST", "/api/v1/scrolls/runtime-a/commands/test", nil)
	request.Header.Set("X-Druid-Runtime-ID", "runtime-a")
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || string(body) != "unsafe:runtime-a" || authenticator.called {
		t.Fatalf("status=%d body=%q authenticator=%#v", response.StatusCode, body, authenticator)
	}
}
