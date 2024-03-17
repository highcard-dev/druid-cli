package services

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/logger"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type ScrollService struct {
	processCwd       string
	scroll           *domain.Scroll
	lock             *domain.ScrollLock
	templateRenderer ports.TemplateRendererInterface
}
type TemplateData struct {
	Config interface{}
}

func NewScrollService(
	processCwd string,
) *ScrollService {
	s := &ScrollService{
		processCwd:       processCwd,
		templateRenderer: NewTemplateRenderer(),
	}

	return s
}

func (sc *ScrollService) LoadScroll() (*domain.Scroll, error) {
	// TODO: better templating for scrolls in next version or so
	os.Setenv("SCROLL_DIR", sc.GetDir())
	scroll, err := domain.NewScroll(sc.GetDir())

	return scroll, err
}

// Load Scroll and render templates in the cwd
func (sc *ScrollService) Bootstrap(ignoreVersionCheck bool) (*domain.Scroll, *domain.ScrollLock, error) {

	scroll, err := sc.LoadScroll()

	if scroll == nil {
		return nil, nil, err
	}

	sc.scroll = scroll

	if !sc.LockExists() {
		return scroll, nil, errors.New("scroll lock not found")
	}

	lock, err := sc.ReadLock()

	if err != nil {
		return scroll, nil, err
	}
	sc.lock = lock

	//Update the lock with the current scroll version
	if lock.ScrollVersion == nil {
		lock.ScrollVersion = scroll.Version
		lock.ScrollName = scroll.Name
		lock.Write()
	} else {
		if !lock.ScrollVersion.Equal(sc.scroll.Version) && !ignoreVersionCheck {
			return scroll, lock, errors.New("scroll version mismatch")
		}
	}

	err = sc.clearInvalidLockfileStatuses()
	return scroll, lock, err

}
func (sc *ScrollService) CreateLockAndBootstrapFiles() error {

	//init-files just get copied over
	initPath := strings.TrimRight(sc.GetDir(), "/") + "/init-files"
	exist, _ := utils.FileExists(initPath)
	if exist {
		err := filepath.Walk(initPath, func(path string, f os.FileInfo, err error) error {
			strippedPath := strings.TrimPrefix(filepath.Clean(path), filepath.Clean(initPath))
			realPath := filepath.Join(sc.processCwd, strippedPath)
			if f.IsDir() {
				if strippedPath == "" {
					return nil
				}
				err := os.MkdirAll(realPath, f.Mode())
				if err != nil {
					return err
				}
			} else {

				b, err := os.ReadFile(path)

				if err != nil {
					return err
				}

				return os.WriteFile(realPath, b, f.Mode())
			}

			return err
		})
		if err != nil {
			return err
		}
	}
	//init-files-template needs to be rendered
	initPath = strings.TrimRight(sc.GetDir(), "/") + "/init-files-template"
	exist, _ = utils.FileExists(initPath)

	files := []string{}

	if exist {
		err := filepath.Walk(initPath, func(path string, f os.FileInfo, err error) error {
			if f.IsDir() {
				err := os.MkdirAll(path, f.Mode())
				if err != nil {
					return err
				}
			} else {
				files = append(files, path)
			}

			return nil
		})
		if len(files) == 0 {
			return nil
		}
		if err != nil {
			return err
		}

		err = sc.templateRenderer.RenderScrollTemplateFiles(files, sc.scroll, sc.processCwd)
		if err != nil {
			return err
		}
	}

	return nil
}

func (sc *ScrollService) LockExists() bool {
	exisits, err := utils.FileExists(sc.GetDir() + "/scroll-lock.json")
	return err == nil && exisits
}

func (sc *ScrollService) ReadLock() (*domain.ScrollLock, error) {
	lock, err := domain.ReadLock(sc.GetDir() + "/scroll-lock.json")

	if err != nil {
		return nil, err
	}
	return lock, nil
}

func (sc *ScrollService) GetLock() (*domain.ScrollLock, error) {
	if sc.lock != nil {
		return sc.lock, nil
	}

	return nil, errors.New("lock not found")
}

func (sc *ScrollService) WriteNewScrollLock() *domain.ScrollLock {
	return domain.WriteNewScrollLock(sc.GetDir() + "/scroll-lock.json")
}

func (sc *ScrollService) GetDir() string {
	return utils.GetScrollDirFromCwd(sc.processCwd)
}

func (sc *ScrollService) GetCwd() string {
	return sc.processCwd
}

func (s ScrollService) GetCurrent() *domain.Scroll {
	return s.scroll
}
func (s ScrollService) GetFile() *domain.File {
	return &s.scroll.File
}

func (s ScrollService) ScrollExists() bool {

	filePath := s.GetDir() + "/scroll.yaml"
	b, err := utils.FileExists(filePath)
	return b && err == nil
}

func (s ScrollService) RenderCwdTemplates() error {
	cwd := s.processCwd

	libRegEx, err := regexp.Compile(`^.+\.(scroll_template)$`)
	if err != nil {
		return err
	}

	files := []string{}
	filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if filepath.Clean(s.GetDir()) == filepath.Clean(path) {
			return filepath.SkipDir // Skip this subdirectory
		}
		if !libRegEx.MatchString(path) {
			return nil
		}
		files = append(files, path)

		return nil
	})

	if len(files) == 0 {
		return nil
	}

	config := TemplateData{Config: s.GetScrollConfig()}

	return s.templateRenderer.RenderScrollTemplateFiles(files, config, "")

}

func (s ScrollService) GetScrollConfig() interface{} {

	var data interface{}

	content := s.GetScrollConfigRawYaml()

	if len(content) == 0 {
		return data
	}

	// Unmarshal the YAML data into the struct
	yaml.Unmarshal(content, &data)

	return data
}

func (s ScrollService) GetScrollConfigRawYaml() []byte {
	path := s.processCwd + "/.scroll_config.yml"

	content, err := os.ReadFile(path)

	if err != nil {
		return []byte{}
	}

	return content
}

// clear stuff statuses that make no sense in the lockfile
func (sc *ScrollService) clearInvalidLockfileStatuses() error {
	if sc.lock == nil {
		return errors.New("lock not loaded")
	}
	for statusCommand := range sc.lock.Statuses {

		process, command := utils.ParseProcessAndCommand(statusCommand)
		_, err := sc.GetCommand(command, process)
		if err != nil {
			delete(sc.lock.Statuses, statusCommand)
			logger.Log().Info("Removed invalid status from lockfile", zap.String("status", statusCommand))
		}
	}
	return sc.lock.Write()
}

func (sc *ScrollService) GetCommand(cmd string, processId string) (*domain.CommandInstructionSet, error) {
	scroll := sc.GetFile()
	//check if we can accually do it before we start
	if ps, ok := scroll.Processes[processId]; ok {
		cmds, ok := ps.Commands[cmd]
		if !ok {
			return nil, errors.New("command " + cmd + " not found")
		}
		return &cmds, nil
	} else {
		return nil, errors.New("process " + processId + " not found")
	}
}
