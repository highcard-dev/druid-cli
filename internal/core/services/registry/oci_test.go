package registry

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	ocidigest "github.com/opencontainers/go-digest"
)

// fakeRegistry returns a plain-HTTP httptest server that implements the bare
// minimum of the OCI Distribution spec so that oras.Copy can complete a push.
func fakeRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	blobs := map[string][]byte{}
	manifests := map[string][]byte{}
	manifestTypes := map[string]string{}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v2/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tags/list") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"tags":[]}`))
			return
		}
		if strings.Contains(r.URL.Path, "/blobs/") {
			digest := strings.Split(r.URL.Path, "/blobs/")[1]
			if data, ok := blobs[digest]; ok {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
				w.Header().Set("Docker-Content-Digest", digest)
				w.WriteHeader(http.StatusOK)
				w.Write(data)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.Contains(r.URL.Path, "/manifests/") {
			ref := strings.Split(r.URL.Path, "/manifests/")[1]
			if data, ok := manifests[ref]; ok {
				w.Header().Set("Content-Type", manifestTypes[ref])
				w.WriteHeader(http.StatusOK)
				w.Write(data)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("HEAD /v2/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/blobs/")
		if len(parts) == 2 {
			digest := parts[1]
			if data, ok := blobs[digest]; ok {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
				w.Header().Set("Docker-Content-Digest", digest)
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		parts = strings.Split(r.URL.Path, "/manifests/")
		if len(parts) == 2 {
			ref := parts[1]
			if data, ok := manifests[ref]; ok {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
				w.Header().Set("Content-Type", manifestTypes[ref])
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	})

	mux.HandleFunc("POST /v2/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", fmt.Sprintf("%s?upload=1", r.URL.Path))
		w.WriteHeader(http.StatusAccepted)
	})

	mux.HandleFunc("PUT /v2/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		digest := r.URL.Query().Get("digest")
		if digest != "" {
			body := make([]byte, r.ContentLength)
			r.Body.Read(body)
			blobs[digest] = body
			w.Header().Set("Docker-Content-Digest", digest)
			w.WriteHeader(http.StatusCreated)
			return
		}
		if strings.Contains(r.URL.Path, "/manifests/") {
			ref := strings.Split(r.URL.Path, "/manifests/")[1]
			body := make([]byte, r.ContentLength)
			r.Body.Read(body)
			bodyDigest := ocidigest.FromBytes(body).String()
			manifests[ref] = body
			manifests[bodyDigest] = body
			manifestTypes[ref] = r.Header.Get("Content-Type")
			manifestTypes[bodyDigest] = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusCreated)
			return
		}
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		w.WriteHeader(http.StatusCreated)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestPushDataChunkPathNotDoubled calls OciClient.Push directly with a
// relative scroll folder containing a data directory, pushing to a fake
// in-process OCI registry. This verifies the data-chunk file paths are
// resolved correctly (store-relative) and do not get doubled.
//
// Regression test for: when --cwd is a relative path like
// ./scrolls/minecraft/1.17, the ORAS file store root is resolved to an
// absolute path internally. Passing the full relative chunkFullPath
// (scrolls/minecraft/1.17/data/<file>) to fs.Add caused the store to look
// for <abs-root>/scrolls/minecraft/1.17/data/<file> which doesn't exist.
func TestPushDataChunkPathNotDoubled(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	srv := fakeRegistry(t)
	// httptest URL is http://127.0.0.1:<port>; strip scheme for oras.
	registryHost := strings.TrimPrefix(srv.URL, "http://")

	// Build a minimal scroll directory tree via a relative path.
	relFolder := filepath.Join("scrolls", "minecraft", "1.17")
	if err := os.MkdirAll(relFolder, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(relFolder, "scroll.yaml"),
		[]byte("name: test\nversion: 0.1.0\napp_version: \"1.17\"\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	// Create a data directory with a file – this is the path that was doubled.
	dataDir := filepath.Join(relFolder, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "server.properties"), []byte("motd=test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an OciClient configured for plain HTTP pointing at our fake registry.
	credStore := NewCredentialStore([]domain.RegistryCredential{})
	client := &OciClient{
		credentialStore: credStore,
		plainHTTP:       true,
	}

	repoRef := registryHost + "/test/scroll"

	_, err := client.Push(relFolder, repoRef, "1.17", map[string]string{}, false, nil)
	if err != nil {
		t.Fatalf("Push failed unexpectedly: %v", err)
	}
}

func TestPushPullExecutableDataChunkPreservesMode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	srv := fakeRegistry(t)
	registryHost := strings.TrimPrefix(srv.URL, "http://")

	relFolder := filepath.Join("scrolls", "lgsm", "arkserver")
	dataDir := filepath.Join(relFolder, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(relFolder, "scroll.yaml"), []byte("name: test\nversion: 0.1.0\napp_version: arkserver\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "arkserver"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	client := &OciClient{
		credentialStore: NewCredentialStore([]domain.RegistryCredential{}),
		plainHTTP:       true,
	}

	repoRef := registryHost + "/test/scroll"
	scrollFile := &domain.File{
		Chunks: []*domain.Chunks{
			{Name: "lgsm-launcher", Path: "arkserver"},
		},
	}
	if _, err := client.Push(relFolder, repoRef, "arkserver", map[string]string{}, false, scrollFile); err != nil {
		t.Fatalf("Push failed unexpectedly: %v", err)
	}

	pullDir := filepath.Join("pull", "arkserver")
	if err := client.PullSelective(pullDir, repoRef+":arkserver", true, nil); err != nil {
		t.Fatalf("Pull failed unexpectedly: %v", err)
	}

	info, err := os.Stat(filepath.Join(pullDir, "data", "arkserver"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0755 {
		t.Fatalf("data/arkserver mode = %v, want 0755", got)
	}
}
