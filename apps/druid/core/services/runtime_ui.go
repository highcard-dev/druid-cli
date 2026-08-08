package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	uiPublishTTL                = 10 * time.Minute
	maxUIPackageSize            = 256 << 20
)

// PublishUIPackage creates durable work for the authenticated dev workload.
// It deliberately does not ask the daemon to read from the workload.
func (s *RuntimeSupervisor) PublishUIPackage(id string, scope string, sourcePath string) (*domain.RuntimeScroll, error) {
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	uiScope, path, err := normalizeUIPackageRequest(scope, sourcePath)
	if err != nil {
		return nil, err
	}
	status, err := s.runtimeBackend.DevStatus(context.Background(), runtimeScroll.Root)
	if err != nil {
		return nil, err
	}
	if status != ports.RuntimeDevStatusReady {
		return nil, fmt.Errorf("Druid Developer Mode must be ready before publishing a UI package (current status: %s)", status)
	}
	requestID, err := newUIPackagePublishID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if runtimeScroll.UIPackagePublishes == nil {
		runtimeScroll.UIPackagePublishes = map[domain.RuntimeUIPackageScope]domain.UIPackagePublish{}
	}
	runtimeScroll.UIPackagePublishes[uiScope] = domain.UIPackagePublish{
		ID: requestID, Scope: uiScope, Path: path, Status: domain.UIPackagePublishPending,
		ExpiresAt: now.Add(uiPublishTTL), UpdatedAt: now,
	}
	if err := s.store.UpdateScroll(runtimeScroll); err != nil {
		return nil, err
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSupervisor) ClaimUIPackagePublish(id, podUID string) (*domain.UIPackagePublish, error) {
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for scope, request := range runtimeScroll.UIPackagePublishes {
		if request.Status == domain.UIPackagePublishCompleted || request.Status == domain.UIPackagePublishFailed {
			continue
		}
		if !now.Before(request.ExpiresAt) {
			request.Status = domain.UIPackagePublishFailed
			request.Error = "publish request expired"
			request.UpdatedAt = now
			runtimeScroll.UIPackagePublishes[scope] = request
			continue
		}
		if request.Status == domain.UIPackagePublishPending || request.ClaimPod == podUID {
			request.Status = domain.UIPackagePublishClaimed
			request.ClaimPod = podUID
			request.UpdatedAt = now
			runtimeScroll.UIPackagePublishes[scope] = request
			if err := s.store.UpdateScroll(runtimeScroll); err != nil {
				return nil, err
			}
			return &request, nil
		}
	}
	if err := s.store.UpdateScroll(runtimeScroll); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *RuntimeSupervisor) PrepareUIPackagePublish(id, requestID, podUID, sha256 string, size int64) (string, error) {
	runtimeScroll, request, err := s.activeUIPackagePublish(id, requestID, podUID)
	if err != nil {
		return "", err
	}
	if !validUIPackageDigest(sha256) || size <= 0 || size > maxUIPackageSize {
		return "", fmt.Errorf("invalid UI package hash or size")
	}
	request.SHA256 = sha256
	request.Size = size
	request.UpdatedAt = time.Now().UTC()
	runtimeScroll.UIPackagePublishes[request.Scope] = *request
	if err := s.store.UpdateScroll(runtimeScroll); err != nil {
		return "", err
	}
	return s.runtimeBackend.PrepareUIPackageUpload(context.Background(), ports.RuntimeUIPackageUploadAction{
		RuntimeID: runtimeScroll.ID, RootRef: runtimeScroll.Root, Scope: request.Scope, SHA256: sha256, Size: size,
	})
}

func (s *RuntimeSupervisor) CompleteUIPackagePublish(id, requestID, podUID, sha256 string, size int64) (*domain.RuntimeScroll, error) {
	runtimeScroll, request, err := s.activeUIPackagePublish(id, requestID, podUID)
	if err != nil {
		return nil, err
	}
	if request.SHA256 != sha256 || request.Size != size {
		return nil, fmt.Errorf("UI package completion does not match prepared upload")
	}
	result, err := s.runtimeBackend.CompleteUIPackageUpload(context.Background(), ports.RuntimeUIPackageUploadAction{
		RuntimeID: runtimeScroll.ID, RootRef: runtimeScroll.Root, Scope: request.Scope, SHA256: sha256, Size: size,
	})
	if err != nil {
		return nil, err
	}
	if runtimeScroll.UIPackages == nil {
		runtimeScroll.UIPackages = domain.RuntimeUIPackages{}
	}
	now := time.Now().UTC()
	runtimeScroll.UIPackages[request.Scope] = domain.RuntimeUIPackage{URL: result.URL, Path: request.Path, SHA256: result.SHA256, UpdatedAt: now}
	request.Status = domain.UIPackagePublishCompleted
	request.UpdatedAt = now
	runtimeScroll.UIPackagePublishes[request.Scope] = *request
	if err := s.store.UpdateScroll(runtimeScroll); err != nil {
		return nil, err
	}
	return s.store.GetScroll(id)
}

func (s *RuntimeSupervisor) FailUIPackagePublish(id, requestID, podUID, message string) error {
	runtimeScroll, request, err := s.activeUIPackagePublish(id, requestID, podUID)
	if err != nil {
		return err
	}
	request.Status = domain.UIPackagePublishFailed
	request.Error = strings.TrimSpace(message)
	request.UpdatedAt = time.Now().UTC()
	runtimeScroll.UIPackagePublishes[request.Scope] = *request
	return s.store.UpdateScroll(runtimeScroll)
}

func (s *RuntimeSupervisor) activeUIPackagePublish(id, requestID, podUID string) (*domain.RuntimeScroll, *domain.UIPackagePublish, error) {
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return nil, nil, err
	}
	for _, request := range runtimeScroll.UIPackagePublishes {
		if request.ID != requestID {
			continue
		}
		if request.Status != domain.UIPackagePublishClaimed || request.ClaimPod != podUID || !time.Now().UTC().Before(request.ExpiresAt) {
			return nil, nil, fmt.Errorf("UI package publish request is not active for this workload")
		}
		return runtimeScroll, &request, nil
	}
	return nil, nil, fmt.Errorf("UI package publish request not found")
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
	if filepath.Ext(cleaned) != ".wasm" || !strings.HasPrefix(cleaned, string(uiScope)+"/") {
		return "", "", fmt.Errorf("ui package path must point to a %s .wasm file", uiScope)
	}
	return uiScope, cleaned, nil
}

func newUIPackagePublishID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func validUIPackageDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
