package services

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils"
	"gopkg.in/yaml.v2"
)

type ScrollService struct {
	scrollDir        string
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
		scrollDir:        processCwd,
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

	//enseure data dir exists
	err = os.MkdirAll(sc.GetCwd(), os.ModePerm)
	if err != nil {
		return nil, err
	}

	sc.scroll = scroll

	return scroll, nil
}

// Load Scroll and render templates in the cwd
func (sc *ScrollService) ReloadLock(ignoreVersionCheck bool) (*domain.ScrollLock, error) {

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
	return sc.scrollDir
}

func (sc *ScrollService) GetCwd() string {
	return utils.GetDataDirFromScrollDir(sc.scrollDir)
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
	cwd := s.scrollDir

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

	return s.templateRenderer.RenderScrollTemplateFiles("", files, config, "")

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
	path := s.scrollDir + "/.scroll_config.yml"

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

func (sc *ScrollService) AddTemporaryCommand(cmd string, instructions *domain.CommandInstructionSet) {
	scroll := sc.GetFile()
	if scroll.Commands == nil {
		scroll.Commands = make(map[string]*domain.CommandInstructionSet)
	}
	scroll.Commands[cmd] = instructions
}
