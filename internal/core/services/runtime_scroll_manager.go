package services

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type RuntimeScrollManager struct {
	store RuntimeScrollStore
}

var ErrScrollAlreadyExists = errors.New("runtime scroll already exists")

func NewRuntimeScrollManager(store RuntimeScrollStore) *RuntimeScrollManager {
	return &RuntimeScrollManager{store: store}
}

func (m *RuntimeScrollManager) Create(artifact string, requestedName string, scrollRoot string, dataRoot string, scrollYAML []byte) (*domain.RuntimeScroll, error) {
	if artifact == "" {
		return nil, fmt.Errorf("artifact is required")
	}
	if scrollRoot == "" {
		return nil, fmt.Errorf("scroll root is required")
	}
	if dataRoot == "" {
		return nil, fmt.Errorf("data root is required")
	}
	if len(scrollYAML) == 0 {
		return nil, fmt.Errorf("scroll yaml is required")
	}
	scroll, err := domain.NewScrollFromBytes(scrollRoot, scrollYAML)
	if err != nil {
		return nil, err
	}
	if err := scroll.Validate(false); err != nil {
		return nil, err
	}
	id, err := RuntimeScrollID(requestedName, scroll.Name)
	if err != nil {
		return nil, err
	}
	if _, err := m.store.GetScroll(id); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrScrollAlreadyExists, id)
	} else if !errors.Is(err, ErrScrollNotFound) {
		return nil, err
	}

	runtimeScroll := &domain.RuntimeScroll{
		ID:         id,
		Artifact:   artifact,
		ScrollRoot: scrollRoot,
		DataRoot:   dataRoot,
		ScrollName: scroll.Name,
		ScrollYAML: string(scrollYAML),
		Status:     domain.RuntimeScrollStatusCreated,
		Commands:   map[string]domain.LockStatus{},
	}
	if err := m.store.CreateScroll(runtimeScroll); err != nil {
		return nil, err
	}
	return runtimeScroll, nil
}

func RuntimeScrollID(requestedName string, scrollName string) (string, error) {
	id := RuntimeScrollIDFromName(requestedName)
	if id == "" {
		id = RuntimeScrollIDFromName(scrollName)
	}
	if id == "" {
		return "", fmt.Errorf("scroll id could not be generated")
	}
	return id, nil
}

func RuntimeScrollIDFromName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	if at := strings.Index(name, "@"); at >= 0 {
		name = name[:at]
	}
	if colon := strings.Index(name, ":"); colon >= 0 {
		name = name[:colon]
	}
	name = strings.ToLower(name)
	name = regexp.MustCompile(`[^a-z0-9_.-]+`).ReplaceAllString(name, "-")
	name = strings.Trim(name, "-_.")
	return name
}

func MaterializeScrollArtifact(artifact string, scrollRoot string, dataRoot string, ociRegistry ports.OciRegistryInterface, includeData bool) error {
	if artifact == "" {
		return fmt.Errorf("artifact is required")
	}
	if scrollRoot == "" {
		return fmt.Errorf("scroll root is required")
	}
	if dataRoot == "" {
		return fmt.Errorf("data root is required")
	}
	if err := os.RemoveAll(scrollRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(scrollRoot, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dataRoot, domain.RuntimeDataDir), 0755); err != nil {
		return err
	}
	if localPathExists(artifact) {
		if err := materializeLocalArtifact(artifact, scrollRoot); err != nil {
			return err
		}
		if scrollRoot == dataRoot {
			return os.MkdirAll(filepath.Join(dataRoot, domain.RuntimeDataDir), 0755)
		}
		return moveRuntimeData(scrollRoot, dataRoot)
	}
	if ociRegistry == nil {
		return fmt.Errorf("OCI registry is required to pull %s", artifact)
	}
	if err := ociRegistry.PullSelective(scrollRoot, artifact, includeData, nil); err != nil {
		return err
	}
	if includeData {
		if scrollRoot == dataRoot {
			return os.MkdirAll(filepath.Join(dataRoot, domain.RuntimeDataDir), 0755)
		}
		return moveRuntimeData(scrollRoot, dataRoot)
	}
	return os.MkdirAll(filepath.Join(dataRoot, domain.RuntimeDataDir), 0755)
}

func moveRuntimeData(scrollRoot string, dataRoot string) error {
	src := filepath.Join(scrollRoot, domain.RuntimeDataDir)
	if !localPathExists(src) {
		return os.MkdirAll(filepath.Join(dataRoot, domain.RuntimeDataDir), 0755)
	}
	dst := filepath.Join(dataRoot, domain.RuntimeDataDir)
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyDir(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func MoveMaterializedScroll(srcScrollRoot string, srcDataRoot string, dstScrollRoot string, dstDataRoot string) error {
	if srcScrollRoot == srcDataRoot && dstScrollRoot == dstDataRoot {
		if localPathExists(dstScrollRoot) {
			return fmt.Errorf("target scroll root already exists: %s", dstScrollRoot)
		}
		if err := os.MkdirAll(filepath.Dir(dstScrollRoot), 0755); err != nil {
			return err
		}
		if err := os.Rename(srcScrollRoot, dstScrollRoot); err != nil {
			if err := copyDir(srcScrollRoot, dstScrollRoot); err != nil {
				return err
			}
			if err := os.RemoveAll(srcScrollRoot); err != nil {
				return err
			}
		}
		return nil
	}
	if localPathExists(dstScrollRoot) {
		return fmt.Errorf("target scroll root already exists: %s", dstScrollRoot)
	}
	if localPathExists(dstDataRoot) {
		return fmt.Errorf("target data root already exists: %s", dstDataRoot)
	}
	if err := os.MkdirAll(filepath.Dir(dstScrollRoot), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dstDataRoot), 0755); err != nil {
		return err
	}
	if err := os.Rename(srcScrollRoot, dstScrollRoot); err != nil {
		if err := copyDir(srcScrollRoot, dstScrollRoot); err != nil {
			return err
		}
		if err := os.RemoveAll(srcScrollRoot); err != nil {
			return err
		}
	}
	if err := os.Rename(srcDataRoot, dstDataRoot); err != nil {
		if err := copyDir(srcDataRoot, dstDataRoot); err != nil {
			return err
		}
		if err := os.RemoveAll(srcDataRoot); err != nil {
			return err
		}
	}
	return nil
}

func materializeLocalArtifact(artifact string, scrollRoot string) error {
	info, err := os.Stat(artifact)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if filepath.Base(artifact) != "scroll.yaml" {
			return fmt.Errorf("local file artifact must be scroll.yaml")
		}
		return copyFile(artifact, filepath.Join(scrollRoot, "scroll.yaml"))
	}
	return copyDir(artifact, scrollRoot)
}

func copyDir(src string, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func localPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
