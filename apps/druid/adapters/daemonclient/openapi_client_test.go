package daemonclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/highcard-dev/daemon/internal/api"
)

func TestCreateScrollDoesNotSendStart(t *testing.T) {
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/scrolls" {
			t.Fatalf("path = %s, want /api/v1/scrolls", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"scroll-a","artifact":"artifact","root":"/root","scroll_name":"scroll","status":"created","created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z"}`))
	}))
	defer server.Close()
	client, err := api.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	openAPIClient := &OpenAPIClient{client: client}

	if _, err := openAPIClient.CreateScroll(t.Context(), "scroll-a", "artifact", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["start"]; ok {
		t.Fatalf("create request should not send start: %#v", got)
	}
}
