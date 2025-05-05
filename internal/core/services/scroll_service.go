package services

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
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

func (sc *ScrollService) InitFiles(fls ...string) error {
	//init-files-template needs to be rendered
	files, err := sc.filterFiles("init-files", fls...)
	if err != nil {
		return err
	}
	initFileDir := path.Join(sc.GetDir(), "init-files")

	for _, file := range files {
		basePath := strings.TrimPrefix(file, initFileDir)
		dest := path.Join(sc.processCwd, basePath)

		err := utils.CopyFile(file, dest)
		if err != nil {
			return err
		}
	}
	return nil
}

func (sc *ScrollService) filterFiles(path string, fls ...string) ([]string, error) {
	//init-files-template needs to be rendered
	initPath := strings.TrimRight(sc.GetDir(), "/") + "/" + path + "/"
	exist, _ := utils.FileExists(initPath)

	files := []string{}

	if exist {
		err := filepath.Walk(initPath, func(path string, f os.FileInfo, err error) error {
			basePath := strings.TrimPrefix(path, initPath)
			if !f.IsDir() && (slices.Contains(fls, basePath) || len(fls) == 0) {
				files = append(files, path)
			}
			return nil
		})

		if err != nil {
			return []string{}, err
		}

		return files, nil
	}

	return []string{}, nil
}

func (sc *ScrollService) InitTemplateFiles(fls ...string) error {

	//init-files-template needs to be rendered
	files, err := sc.filterFiles("init-files-template", fls...)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	templateBase := path.Join(sc.GetDir(), "init-files-template")

	for i, file := range files {
		basePath := strings.TrimPrefix(file, templateBase)
		files[i] = basePath
	}

	return sc.templateRenderer.RenderScrollTemplateFiles(templateBase, files, sc.scroll, sc.processCwd)
}

func (sc *ScrollService) CopyingInitFiles() error {
	err := sc.InitFiles()
	if err != nil {
		return err
	}
	return sc.InitTemplateFiles()
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
