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
) (*ScrollService, error) {
	s := &ScrollService{
		processCwd:       processCwd,
		templateRenderer: NewTemplateRenderer(),
	}

	_, err := s.ReloadScroll()

	return s, err
}

func (sc *ScrollService) ReloadScroll() (*domain.Scroll, error) {
	// TODO: better templating for scrolls in next version or so
	os.Setenv("SCROLL_DIR", sc.GetDir())
	scroll, err := domain.NewScroll(sc.GetDir())

	if err != nil {
		return nil, err
	}

	sc.scroll = scroll

	return scroll, nil
}

// Load Scroll and render templates in the cwd
func (sc *ScrollService) Bootstrap(ignoreVersionCheck bool) (*domain.ScrollLock, error) {

	var scroll = sc.scroll

	lock := sc.ReadLock()

	sc.lock = lock

	//Update the lock with the current scroll version
	if lock.ScrollVersion == nil {
		lock.ScrollVersion = scroll.Version
		lock.ScrollName = scroll.Name
		lock.Write()
	} else {
		if !lock.ScrollVersion.Equal(sc.scroll.Version) && !ignoreVersionCheck {
			return lock, errors.New("scroll version mismatch")
		}
	}

	return lock, nil

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

func (sc *ScrollService) ReadLock() *domain.ScrollLock {
	lock, err := domain.ReadLock(sc.GetDir() + "/scroll-lock.json")

	if err != nil {
		return sc.WriteNewScrollLock()
	}
	return lock
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

func (sc *ScrollService) GetCommand(cmd string) (*domain.CommandInstructionSet, error) {
	scroll := sc.GetFile()
	//check if we can accually do it before we start
	if cmds, ok := scroll.Commands[cmd]; ok {
		return cmds, nil
	} else {
		return nil, errors.New("command " + cmd + " not found")
	}
}
