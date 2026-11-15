package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchServiceReportsOnlyConfiguredFiles(t *testing.T) {
	root := t.TempDir()
	wasm := "private/dist/app.wasm"
	document := "private/dist/view.openui"
	service := NewWatchService(root)
	service.StartWatching([]string{wasm, document})
	t.Cleanup(service.StopWatching)
	events := service.Subscribe()

	write := func(path, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	assertChange := func(path, operation string) {
		t.Helper()
		select {
		case raw := <-events:
			var event FileChangeEvent
			if err := json.Unmarshal(*raw, &event); err != nil {
				t.Fatal(err)
			}
			if event.Path != path || event.Operation != operation {
				t.Fatalf("got %s %s, want %s %s", event.Operation, event.Path, operation, path)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for %s %s", operation, path)
		}
	}

	if err := os.MkdirAll(filepath.Join(root, "private", "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("private/source.rs", "source")
	write("private/dist/other.wasm", "unrelated output")
	write(wasm, "v1")
	write(document, "doc")
	assertChange(wasm, "CREATE")
	assertChange(document, "CREATE")

	write("private/dist/temporary.wasm", "v2")
	if err := os.Rename(filepath.Join(root, "private/dist/temporary.wasm"), filepath.Join(root, wasm)); err != nil {
		t.Fatal(err)
	}
	assertChange(wasm, "WRITE")

	write(document, "new layout")
	assertChange(document, "WRITE")

	service.StopWatching()
	if _, ok := <-events; ok || service.IsWatching() {
		t.Fatal("stop did not close the subscriptions")
	}
}
