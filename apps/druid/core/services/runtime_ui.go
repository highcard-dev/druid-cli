package services

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

// PrepareUIPackageUpload binds a one-shot S3 capability to digests calculated
// by the authenticated publish Job. The pending request was created by the
// owner-authorized publish action and is removed when that action completes.
func (s *RuntimeSupervisor) PrepareUIPackageUpload(id string, scope string, requestID string, sha256 string, contentMD5 string) (string, error) {
	uiScope, _, err := normalizeUIPackageRequest(scope, "")
	if err != nil {
		return "", err
	}
	if sum, err := hex.DecodeString(sha256); err != nil || len(sum) != 32 {
		return "", fmt.Errorf("invalid UI package SHA-256")
	}
	if sum, err := base64.StdEncoding.DecodeString(contentMD5); err != nil || len(sum) != 16 {
		return "", fmt.Errorf("invalid UI package Content-MD5")
	}
	s.mu.Lock()
	action, ok := s.uiPublishes[requestID]
	if !ok || action.RuntimeID != id || action.Scope != uiScope || action.SHA256 != "" || action.ContentMD5 != "" {
		s.mu.Unlock()
		return "", fmt.Errorf("unknown or already prepared UI package upload")
	}
	action.SHA256 = sha256
	action.ContentMD5 = contentMD5
	s.uiPublishes[requestID] = action
	s.mu.Unlock()

	url, err := s.runtimeBackend.PrepareUIPackageUpload(context.Background(), action)
	if err != nil {
		s.mu.Lock()
		delete(s.uiPublishes, requestID)
		s.mu.Unlock()
		return "", err
	}
	return url, nil
}

const (
	defaultPrivateUIPackagePath = "private/dist/app.wasm"
	defaultPublicUIPackagePath  = "public/dist/app.wasm"
	uiPublishImage              = "curlimages/curl:8.12.1"
)

// PublishUIPackage runs the same one-shot command lifecycle as other runtime
// actions. The URL is an expiring, exact-object capability and is never
// persisted in the deployment definition.
func (s *RuntimeSupervisor) PublishUIPackage(id string, scope string, sourcePath string) (*domain.RuntimeScroll, error) {
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	uiScope, sourcePath, err := normalizeUIPackageRequest(scope, sourcePath)
	if err != nil {
		return nil, err
	}
	if s.workerDaemonURL == "" {
		return nil, fmt.Errorf("Druid UI publishing requires a configured runtime management URL")
	}
	status, err := s.runtimeBackend.DevStatus(context.Background(), runtimeScroll.Root)
	if err != nil {
		return nil, err
	}
	if status != ports.RuntimeDevStatusReady {
		return nil, fmt.Errorf("Druid Developer Mode must be ready before publishing a UI package (current status: %s)", status)
	}

	action := ports.RuntimeUIPackageUploadAction{
		RuntimeID: runtimeScroll.ID,
		RootRef:   runtimeScroll.Root,
		Scope:     uiScope,
		RequestID: uuid.NewString(),
	}
	s.mu.Lock()
	s.uiPublishes[action.RequestID] = action
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.uiPublishes, action.RequestID)
		s.mu.Unlock()
	}()

	commandName := "ui_publish_" + string(uiScope)
	command := uiPublishCommand()
	session, err := s.sessionFor(runtimeScroll.ID)
	if err != nil {
		return nil, err
	}
	if err := session.AddCommand(commandName, command); err != nil {
		return nil, err
	}
	procedureName := domain.ProcedureName(commandName, 0, command.Procedures[0])
	if err := session.AddTempItemWithWaitEnv(commandName, map[string]map[string]string{
		procedureName: {
			"DRUID_UI_SOURCE":      sourcePath,
			"DRUID_UI_REQUEST_ID":  action.RequestID,
			"DRUID_UI_PREPARE_URL": strings.TrimRight(s.workerDaemonURL, "/") + "/api/v1/scrolls/" + runtimeScroll.ID + "/ui/packages/" + string(uiScope) + "/prepare",
		},
	}); err != nil {
		return nil, err
	}

	s.mu.Lock()
	action = s.uiPublishes[action.RequestID]
	s.mu.Unlock()
	if action.SHA256 == "" || action.ContentMD5 == "" {
		return nil, fmt.Errorf("Druid UI publish job did not request its digest-bound upload URL")
	}
	result, err := s.runtimeBackend.CompleteUIPackageUpload(context.Background(), action)
	if err != nil {
		return nil, fmt.Errorf("verify uploaded UI package: %w", err)
	}
	runtimeScroll, err = s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	if runtimeScroll.UIPackages == nil {
		runtimeScroll.UIPackages = domain.RuntimeUIPackages{}
	}
	runtimeScroll.UIPackages[uiScope] = domain.RuntimeUIPackage{
		URL: result.URL, Path: sourcePath, SHA256: result.SHA256, UpdatedAt: time.Now().UTC(),
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
prepare_url="${DRUID_UI_PREPARE_URL:?DRUID_UI_PREPARE_URL is required}"
request_id="${DRUID_UI_REQUEST_ID:?DRUID_UI_REQUEST_ID is required}"
token_file="${DRUID_UI_TOKEN_FILE:-/var/run/secrets/druid-cli/token}"
case "$source" in private/*.wasm|public/*.wasm) ;; *) echo "invalid UI package path" >&2; exit 1;; esac
test -f "$source" && test -s "$source"
artifact="$(mktemp)"
trap 'rm -f "$artifact"' EXIT
cp "$source" "$artifact"
source="$artifact"
sha256="$(sha256sum "$source" | awk '{print $1}')"
content_md5="$(md5sum "$source" | awk '{print $1}' | xxd -r -p | base64 | tr -d '\n')"
url="$(curl --fail --silent --show-error --request POST \
  --header "Authorization: Bearer $(cat "$token_file")" \
  --data-urlencode "request_id=$request_id" \
  --data-urlencode "sha256=$sha256" \
  --data-urlencode "content_md5=$content_md5" \
  "$prepare_url")"
curl --fail --silent --show-error --request PUT --upload-file "$source" \
  --header "Content-Type: application/wasm" \
  --header "Cache-Control: public, max-age=31536000, immutable" \
  --header "x-amz-sdk-checksum-algorithm: SHA256" \
  --header "x-amz-checksum-sha256: $(printf '%s' "$sha256" | xxd -r -p | base64 | tr -d '\n')" \
  --header "Content-MD5: $content_md5" \
  "$url"
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
