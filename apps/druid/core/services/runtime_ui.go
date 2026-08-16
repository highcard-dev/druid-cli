package services

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

const (
	defaultPrivateUIPackagePath = "private/dist/app.wasm"
	defaultPublicUIPackagePath  = "public/dist/app.wasm"
	uiPublishImage              = "curlimages/curl:8.12.1"
)

// PublishUIPackage runs a one-shot command with a short-lived upload URL for a
// unique, immutable object. The URL is supplied only to that command.
func (s *RuntimeSupervisor) PublishUIPackage(id string, scope string, sourcePath string) (*domain.RuntimeScroll, error) {
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	uiScope, sourcePath, err := normalizeUIPackageRequest(scope, sourcePath)
	if err != nil {
		return nil, err
	}
	action := ports.RuntimeUIPackageUploadAction{
		RuntimeID: runtimeScroll.ID,
		RootRef:   runtimeScroll.Root,
		Scope:     uiScope,
		RequestID: uuid.NewString(),
	}
	upload, err := s.runtimeBackend.CreateUIPackageUpload(context.Background(), action)
	if err != nil {
		return nil, fmt.Errorf("create UI package upload: %w", err)
	}
	if upload.UploadURL == "" || upload.URL == "" {
		return nil, fmt.Errorf("UI package upload is incomplete")
	}

	commandName := "ui_publish_" + string(uiScope) + "_" + strings.ReplaceAll(action.RequestID, "-", "")[:12]
	command := uiPublishCommand()
	session, err := s.sessionFor(runtimeScroll.ID)
	if err != nil {
		return nil, err
	}
	if err := session.AddCommand(commandName, command); err != nil {
		return nil, err
	}
	defer func() { _ = session.RemoveCommand(commandName) }()
	procedureName := domain.ProcedureName(commandName, 0, command.Procedures[0])
	if err := session.AddTempItemWithWaitEnv(commandName, map[string]map[string]string{
		procedureName: {
			"DRUID_UI_SOURCE":     sourcePath,
			"DRUID_UI_UPLOAD_URL": upload.UploadURL,
		},
	}); err != nil {
		return nil, err
	}
	runtimeScroll, err = s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	if runtimeScroll.UIPackages == nil {
		runtimeScroll.UIPackages = domain.RuntimeUIPackages{}
	}
	runtimeScroll.UIPackages[uiScope] = domain.RuntimeUIPackage{
		URL: upload.URL, Path: sourcePath, UpdatedAt: time.Now().UTC(),
	}
	if err := s.store.UpdateScroll(runtimeScroll); err != nil {
		return nil, err
	}
	return s.store.GetScroll(id)
}

func uiPublishCommand() *domain.CommandInstructionSet {
	return &domain.CommandInstructionSet{
		Run: domain.RunModeOnce,
		Procedures: []*domain.Procedure{{
			Image:      uiPublishImage,
			WorkingDir: "/app/resources/deployment",
			Mounts:     []domain.Mount{{Path: "/app/resources/deployment"}},
			Command: []string{"sh", "-ec", `
source="${DRUID_UI_SOURCE:?DRUID_UI_SOURCE is required}"
upload_url="${DRUID_UI_UPLOAD_URL:?DRUID_UI_UPLOAD_URL is required}"
case "$source" in private/*.wasm|public/*.wasm) ;; *) echo "invalid UI package path" >&2; exit 1;; esac
test -f "$source" && test -s "$source"
echo "Uploading Druid UI package"
curl --fail-with-body --silent --show-error --request PUT --upload-file "$source" \
  --header "Content-Type: application/wasm" \
  --header "Cache-Control: public, max-age=31536000, immutable" \
  "$upload_url"
`},
		}},
	}
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
