//go:build integration && kubernetes

package kubernetes_test

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/highcard-dev/daemon/test/integration/internal/e2e"
)

func TestRegistryCandidateResolution(t *testing.T) {
	bins := e2e.BuildBinaries(t)
	port := e2e.StartRegistry(t)
	fixture := e2e.WriteFixture(t, filepath.Join(t.TempDir(), "scroll"), "registry-resolution", 8080, 8081)
	artifact := fmt.Sprintf("127.0.0.1:%d/druid-e2e/registry-resolution:v1", port)
	e2e.RunEnv(t, []string{"DRUID_REGISTRY_PLAIN_HTTP=true"}, bins.Druid, "push", artifact, fixture.Dir)
	resolveRegistryCandidate(t, fmt.Sprintf("http://127.0.0.1:%d/v2/druid-e2e/registry-resolution/manifests/v1", port))
}

func resolveRegistryCandidate(t *testing.T, url string) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
	digest := response.Header.Get("Docker-Content-Digest")
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("candidate lookup: status=%d body=%s", response.StatusCode, body)
	}
	return digest
}
