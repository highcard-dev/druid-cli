package utils

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func TestAutoChunkDataDirExplicitChunksKeepPlainPaths(t *testing.T) {
	dataDir := t.TempDir()
	mkdirAll(t, filepath.Join(dataDir, "serverfiles", "Engine"))
	writeFile(t, filepath.Join(dataDir, "install-lgsm.sh"), "install\n")

	chunks, err := AutoChunkDataDir(dataDir, []*domain.Chunks{
		{Name: "install-lgsm", Path: "install-lgsm.sh"},
		{Name: "server-engine", Path: "serverfiles/Engine"},
	})
	if err != nil {
		t.Fatalf("AutoChunkDataDir returned error: %v", err)
	}

	assertChunkPaths(t, chunks, []string{"install-lgsm.sh", "serverfiles/Engine"})
	assertChunkNames(t, chunks, []string{"install-lgsm", "server-engine"})
}

func TestAutoChunkDataDirExpandsGlobbedChunkPaths(t *testing.T) {
	dataDir := t.TempDir()
	mkdirAll(t, filepath.Join(dataDir, "serverfiles", "ShooterGame", "Content", "Aberration"))
	writeFile(t, filepath.Join(dataDir, "serverfiles", "ShooterGame", "Content", "Cookinfo.bin"), "cookinfo\n")

	chunks, err := AutoChunkDataDir(dataDir, []*domain.Chunks{
		{Name: "content", Path: "serverfiles/ShooterGame/Content/*"},
	})
	if err != nil {
		t.Fatalf("AutoChunkDataDir returned error: %v", err)
	}

	assertChunkPaths(t, chunks, []string{
		"serverfiles/ShooterGame/Content/Aberration",
		"serverfiles/ShooterGame/Content/Cookinfo.bin",
	})
	assertChunkNames(t, chunks, []string{"content-aberration", "content-cookinfo-bin"})
}

func TestAutoChunkDataDirExpandsNestedChunksWithParentRemainder(t *testing.T) {
	dataDir := t.TempDir()
	contentDir := filepath.Join(dataDir, "serverfiles", "ShooterGame", "Content")
	mkdirAll(t, filepath.Join(contentDir, "Aberration"))
	writeFile(t, filepath.Join(contentDir, "Cookinfo.bin"), "cookinfo\n")
	mkdirAll(t, filepath.Join(contentDir, "Maps", "TheIsland"))
	mkdirAll(t, filepath.Join(contentDir, "Mods", "Ragnarok"))
	mkdirAll(t, filepath.Join(contentDir, "Movies"))
	writeFile(t, filepath.Join(contentDir, "Movies", "ARKTitle.mp4"), "movie\n")

	chunks, err := AutoChunkDataDir(dataDir, []*domain.Chunks{
		{
			Name: "content",
			Path: "serverfiles/ShooterGame/Content",
			Chunks: []*domain.Chunks{
				{Name: "maps", Path: "Maps/*"},
				{Name: "mods", Path: "Mods/*"},
				{Name: "movies", Path: "Movies/*"},
			},
		},
	})
	if err != nil {
		t.Fatalf("AutoChunkDataDir returned error: %v", err)
	}

	assertChunkPaths(t, chunks, []string{
		"serverfiles/ShooterGame/Content/Aberration",
		"serverfiles/ShooterGame/Content/Cookinfo.bin",
		"serverfiles/ShooterGame/Content/Maps/TheIsland",
		"serverfiles/ShooterGame/Content/Mods/Ragnarok",
		"serverfiles/ShooterGame/Content/Movies/ARKTitle.mp4",
	})
	assertChunkNames(t, chunks, []string{
		"content-aberration",
		"content-cookinfo-bin",
		"maps",
		"mods",
		"movies",
	})
}

func TestAutoChunkDataDirRejectsGlobWithoutMatches(t *testing.T) {
	dataDir := t.TempDir()

	_, err := AutoChunkDataDir(dataDir, []*domain.Chunks{
		{Name: "missing", Path: "serverfiles/ShooterGame/Content/*"},
	})
	if err == nil || !strings.Contains(err.Error(), "glob matched no files") {
		t.Fatalf("expected missing glob error, got %v", err)
	}
}

func TestAutoChunkDataDirRejectsOverlappingChunks(t *testing.T) {
	dataDir := t.TempDir()
	mkdirAll(t, filepath.Join(dataDir, "serverfiles", "ShooterGame", "Content", "Maps"))

	_, err := AutoChunkDataDir(dataDir, []*domain.Chunks{
		{Name: "content", Path: "serverfiles/ShooterGame/Content"},
		{Name: "maps", Path: "serverfiles/ShooterGame/Content/Maps"},
	})
	if err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("expected overlap error, got %v", err)
	}
}

func TestAutoChunkDataDirRejectsSiblingGlobOverlap(t *testing.T) {
	dataDir := t.TempDir()
	mkdirAll(t, filepath.Join(dataDir, "serverfiles", "ShooterGame", "Content", "Maps", "TheIsland"))

	_, err := AutoChunkDataDir(dataDir, []*domain.Chunks{
		{Name: "content", Path: "serverfiles/ShooterGame/Content/*"},
		{Name: "maps", Path: "serverfiles/ShooterGame/Content/Maps/*"},
	})
	if err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("expected overlap error, got %v", err)
	}
}

func TestAutoChunkDataDirValidatesSymlinksAfterExpansion(t *testing.T) {
	dataDir := t.TempDir()
	contentDir := filepath.Join(dataDir, "serverfiles", "ShooterGame", "Content")
	mkdirAll(t, filepath.Join(contentDir, "Maps", "TheIsland"))
	if err := os.Symlink(filepath.Join("Maps", "TheIsland"), filepath.Join(contentDir, "CurrentMap")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	_, err := AutoChunkDataDir(dataDir, []*domain.Chunks{
		{
			Name: "content",
			Path: "serverfiles/ShooterGame/Content",
			Chunks: []*domain.Chunks{
				{Name: "maps", Path: "Maps/*"},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "cross-chunk symlinks") {
		t.Fatalf("expected cross-chunk symlink error, got %v", err)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("failed to create directory %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

func assertChunkPaths(t *testing.T, chunks []*domain.Chunks, want []string) {
	t.Helper()
	got := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		got = append(got, chunk.Path)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chunk paths mismatch\ngot:  %v\nwant: %v", got, want)
	}
}

func assertChunkNames(t *testing.T, chunks []*domain.Chunks, want []string) {
	t.Helper()
	got := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		got = append(got, chunk.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chunk names mismatch\ngot:  %v\nwant: %v", got, want)
	}
}
