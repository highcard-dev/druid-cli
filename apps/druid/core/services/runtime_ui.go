package services

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

const (
	defaultPrivateUIPackagePath = "private/dist/app.wasm"
	defaultPublicUIPackagePath  = "public/dist/app.wasm"
)

func (s *RuntimeSupervisor) PublishUIPackage(id string, scope string, sourcePath string) (*domain.RuntimeScroll, error) {
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	uiScope, path, err := normalizeUIPackageRequest(scope, sourcePath)
	if err != nil {
		return nil, err
	}
	result, err := s.runtimeBackend.PublishUIPackage(context.Background(), ports.RuntimeUIPackageAction{
		RuntimeID:  runtimeScroll.ID,
		RootRef:    runtimeScroll.Root,
		Scope:      uiScope,
		SourcePath: path,
	})
	if err != nil {
		return nil, err
	}
	if runtimeScroll.UIPackages == nil {
		runtimeScroll.UIPackages = domain.RuntimeUIPackages{}
	}
	runtimeScroll.UIPackages[uiScope] = domain.RuntimeUIPackage{
		URL:       result.URL,
		Path:      result.Path,
		SHA256:    result.SHA256,
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.store.UpdateScroll(runtimeScroll); err != nil {
		return nil, err
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSupervisor) UIPackages(id string) (domain.RuntimeUIPackages, error) {
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	if runtimeScroll.UIPackages == nil {
		return domain.RuntimeUIPackages{}, nil
	}
	return runtimeScroll.UIPackages, nil
}

func normalizeUIPackageRequest(scope string, sourcePath string) (domain.RuntimeUIPackageScope, string, error) {
	uiScope := domain.RuntimeUIPackageScope(strings.ToLower(strings.TrimSpace(scope)))
	if uiScope != domain.RuntimeUIPackageScopePrivate && uiScope != domain.RuntimeUIPackageScopePublic {
		return "", "", fmt.Errorf("ui package scope must be private or public")
	}
	if sourcePath == "" {
		if uiScope == domain.RuntimeUIPackageScopePublic {
			sourcePath = defaultPublicUIPackagePath
		} else {
			sourcePath = defaultPrivateUIPackagePath
		}
	}
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(sourcePath, "/")))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || filepath.IsAbs(sourcePath) {
		return "", "", fmt.Errorf("ui package path must stay inside the scroll root")
	}
	if strings.HasPrefix(cleaned, "data/private/") || strings.HasPrefix(cleaned, "data/public/") {
		return "", "", fmt.Errorf("ui package path must be rooted at private/ or public/, not data/")
	}
	if filepath.Ext(cleaned) != ".wasm" {
		return "", "", fmt.Errorf("ui package path must point to a .wasm file")
	}
	return uiScope, cleaned, nil
}
