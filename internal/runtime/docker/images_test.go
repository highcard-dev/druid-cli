package docker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/client"
)

func TestPullImageRefreshesRegistryTags(t *testing.T) {
	digestImage := "druid@sha256:" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		name       string
		image      string
		cached     bool
		pullBody   string
		pullStatus int
		wantPulls  int
		wantError  string
	}{
		{name: "stable refreshes cached image", image: "registry.example/druid:stable", cached: true, wantPulls: 1},
		{name: "version tags also refresh", image: "alpine:3.20", cached: true, wantPulls: 1},
		{name: "digest reuses cache", image: digestImage, cached: true},
		{name: "local image reuses cache", image: "druid:local", cached: true},
		{name: "missing digest pulls", image: digestImage, wantPulls: 1},
		{name: "registry failure does not use stale cache", image: "druid:stable", cached: true, pullStatus: http.StatusInternalServerError, pullBody: `{"message":"registry unavailable"}`, wantPulls: 1, wantError: "registry unavailable"},
		{name: "stream failure does not use stale cache", image: "druid:stable", cached: true, pullBody: `{"error":"access denied"}`, wantPulls: 1, wantError: "access denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pulls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/json"):
					if !tc.cached {
						w.WriteHeader(http.StatusNotFound)
						fmt.Fprint(w, `{"message":"image not found"}`)
						return
					}
					fmt.Fprint(w, `{"Id":"sha256:cached"}`)
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create"):
					pulls++
					status := tc.pullStatus
					if status == 0 {
						status = http.StatusOK
					}
					w.WriteHeader(status)
					if tc.pullBody != "" {
						fmt.Fprint(w, tc.pullBody)
					} else {
						fmt.Fprint(w, `{"status":"downloaded current image"}`)
					}
				default:
					t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			cli, err := client.NewClientWithOpts(client.WithHost(server.URL), client.WithVersion("1.47"))
			if err != nil {
				t.Fatal(err)
			}
			defer cli.Close()
			backend := &Backend{client: cli}
			err = backend.pullImage(context.Background(), tc.image)
			if tc.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
			if pulls != tc.wantPulls {
				t.Fatalf("pull requests = %d, want %d", pulls, tc.wantPulls)
			}
		})
	}
}
