package services

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

const (
	uiPublishImage = "curlimages/curl:8.12.1"
)

// PublishUIPackage publishes a single scope through the same one-command upload path.
func (s *RuntimeSupervisor) PublishUIPackage(id string, scope string, withBuild bool) (*domain.RuntimeScroll, error) {
	if scope != "private" && scope != "public" {
		return nil, toolError(400, "ui package scope must be private or public")
	}
	scroll, err := s.ScrollFile(id)
	if err != nil {
		return nil, err
	}
	var declaration *domain.UIPackageDeclaration
	if scroll.UI != nil {
		if scope == "private" {
			declaration = scroll.UI.Private
		} else {
			declaration = scroll.UI.Public
		}
	}
	if declaration == nil {
		return nil, toolError(400, "UI scope is not declared")
	}
	if withBuild && declaration.BuildCommand == "" {
		return nil, toolError(400, "UI scope has no buildcommand")
	}
	return s.publishUIPackages(id, map[domain.RuntimeUIPackageScope]*domain.UIPackageDeclaration{domain.RuntimeUIPackageScope(scope): declaration}, withBuild)
}

// All declared pairs share one container; references change only after all uploads succeed.
func (s *RuntimeSupervisor) publishUIPackages(id string, declarations map[domain.RuntimeUIPackageScope]*domain.UIPackageDeclaration, withBuild bool) (*domain.RuntimeScroll, error) {
	runtimeScroll, err := s.store.GetScroll(id)
	if err != nil {
		return nil, err
	}
	requestID := uuid.NewString()
	scopes := []domain.RuntimeUIPackageScope{}
	for scope := range declarations {
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)
	env := map[string]string{}
	packages := domain.RuntimeUIPackages{}
	for _, scope := range scopes {
		declaration := declarations[scope]
		upload, err := s.runtimeBackend.CreateUIPackageUpload(context.Background(), ports.RuntimeUIPackageUploadAction{
			RuntimeID: runtimeScroll.ID, RootRef: runtimeScroll.Root, Scope: scope, RequestID: requestID,
		})
		if err != nil {
			return nil, fmt.Errorf("create UI package upload: %w", err)
		}
		if upload.UploadURL == "" || upload.DocumentUploadURL == "" || upload.URL == "" {
			return nil, fmt.Errorf("UI package upload is incomplete")
		}
		prefix := "DRUID_UI_" + strings.ToUpper(string(scope)) + "_"
		env[prefix+"SOURCE"] = declaration.WASM
		env[prefix+"DOCUMENT_SOURCE"] = declaration.OpenUI
		env[prefix+"UPLOAD_URL"] = upload.UploadURL
		env[prefix+"DOCUMENT_UPLOAD_URL"] = upload.DocumentUploadURL
		packages[scope] = domain.RuntimeUIPackage{URL: upload.URL, Path: declaration.WASM, OpenUIURL: strings.TrimSuffix(upload.URL, ".wasm") + ".openui", OpenUIPath: declaration.OpenUI, UpdatedAt: time.Now().UTC()}
	}
	commandName := "ui_publish_" + strings.ReplaceAll(requestID, "-", "")[:12]
	command := uiPublishCommand(scopes)
	if withBuild {
		commandName = "publish_with_build_" + strings.ReplaceAll(requestID, "-", "")[:12]
		for _, scope := range scopes {
			command.Needs = append(command.Needs, declarations[scope].BuildCommand)
		}
	}
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, err
	}
	// Explicitly request a fresh build; needs alone accepts previously completed dependencies.
	for _, dependency := range command.Needs {
		if err := session.AddForcedItem(dependency); err != nil {
			return nil, err
		}
	}
	if err := session.AddCommand(commandName, command); err != nil {
		return nil, err
	}
	defer func() { _ = session.RemoveCommand(commandName) }()
	procedureName := domain.ProcedureName(commandName, 0, command.Procedures[0])
	if err := session.AddTempItemWithWaitEnv(commandName, map[string]map[string]string{procedureName: env}); err != nil {
		return nil, err
	}
	// Persist through the live session so subsequent status/command writes retain the pair.
	session.mu.Lock()
	previous := session.runtimeScroll.UIPackages
	updated := domain.RuntimeUIPackages{}
	for scope, pkg := range previous {
		updated[scope] = pkg
	}
	for scope, pkg := range packages {
		updated[scope] = pkg
	}
	session.runtimeScroll.UIPackages = updated
	err = s.store.UpdateScroll(session.runtimeScroll)
	if err != nil {
		session.runtimeScroll.UIPackages = previous
	}
	session.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.store.GetScroll(id)
}

func uiPublishCommand(scopes []domain.RuntimeUIPackageScope) *domain.CommandInstructionSet {
	var checks, uploads strings.Builder
	for _, scope := range scopes {
		prefix := "DRUID_UI_" + strings.ToUpper(string(scope)) + "_"
		// Literal printenv references survive the Scroll environment expansion.
		fmt.Fprintf(&checks, `
case "$(printenv %[1]sSOURCE)" in %[2]s/*.wasm) ;; *) echo "invalid UI package path" >&2; exit 1;; esac
test -s "$(printenv %[1]sSOURCE)"
test -s "$(printenv %[1]sDOCUMENT_SOURCE)"
test -n "$(printenv %[1]sUPLOAD_URL)"
test -n "$(printenv %[1]sDOCUMENT_UPLOAD_URL)"
`, prefix, scope)
		fmt.Fprintf(&uploads, `
curl --fail-with-body --silent --show-error --request PUT --upload-file "$(printenv %[1]sDOCUMENT_SOURCE)" \
  --header "Content-Type: text/plain; charset=utf-8" \
  --header "Cache-Control: public, max-age=31536000, immutable" \
  "$(printenv %[1]sDOCUMENT_UPLOAD_URL)"
curl --fail-with-body --silent --show-error --request PUT --upload-file "$(printenv %[1]sSOURCE)" \
  --header "Content-Type: application/wasm" \
  --header "Cache-Control: public, max-age=31536000, immutable" \
  "$(printenv %[1]sUPLOAD_URL)"
`, prefix)
	}
	return &domain.CommandInstructionSet{Run: domain.RunModeOnce, Procedures: []*domain.Procedure{{
		Image: uiPublishImage, WorkingDir: "/app/resources/deployment",
		Mounts:  []domain.Mount{{Path: "/app/resources/deployment", SubPath: ".", ReadOnly: true}},
		Command: []string{"sh", "-ec", checks.String() + uploads.String()},
	}}}
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
