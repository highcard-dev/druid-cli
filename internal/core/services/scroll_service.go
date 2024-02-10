package services

import (
	"errors"
	"io/ioutil"
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
	lock             *domain.ScrollLock
	scroll           *domain.Scroll
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
	s.lock = domain.NewScrollLock(s.GetDir() + "/scroll-lock.json")

	return s
}

func (sc *ScrollService) LoadScrollWithLockfile() (*domain.Scroll, error) {
	// TODO: better templating for scrolls in next version or so
	os.Setenv("SCROLL_DIR", sc.GetDir())
	scroll, err := domain.NewScroll(sc.GetDir())

	return scroll, err
}

// return at first
// TODO implement multiple scroll support
// To do this, best is to loop over activescrolldir and read every scroll
// TODO: remove initCommandsIdentifiers
func (sc *ScrollService) Bootstrap(ignoreVersionCheck bool) (*domain.Scroll, error) {

	scroll, err := sc.LoadScrollWithLockfile()

	if scroll == nil {
		return nil, err
	}

	sc.scroll = scroll
	err = sc.CheckAndCreateLockFile(ignoreVersionCheck)

	if err != nil {
		return nil, err
	}

	err = sc.RenderCwdTemplates()

	return scroll, err

}

func (sc *ScrollService) CheckAndCreateLockFile(ignoreVersionCheck bool) error {
	exist := sc.lock.LockExists()
	if !exist {
		sc.lock.Statuses = make(map[string]string)
		sc.lock.ScrollVersion = sc.scroll.Version
		sc.lock.ScrollName = sc.scroll.Name
		for key := range sc.scroll.Processes {
			sc.lock.Statuses[key] = "stopped"
		}
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

					b, err := ioutil.ReadFile(path)

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

	} else {
		lock, err := sc.lock.Read()
		sc.lock = lock
		if err != nil {
			return err
		}
		if !sc.lock.ScrollVersion.Equal(sc.scroll.Version) && !ignoreVersionCheck {
			return errors.New("scroll version mismatch")
		}
	}

	sc.lock.Write()
	return nil
}

func (sc *ScrollService) ChangeLockStatus(process string, status string) error {
	if _, ok := sc.lock.Statuses[process]; ok {
		sc.lock.Statuses[process] = status
		sc.lock.Write()
		return nil
	}
	return errors.New("process not found")
}

func (sc *ScrollService) GetLock() *domain.ScrollLock {
	return sc.lock
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
	path := s.processCwd + "/.scroll_config.yml"

	var data interface{}

	content, err := os.ReadFile(path)

	if err != nil {
		return data
	}

	// Unmarshal the YAML data into the struct
	yaml.Unmarshal(content, &data)

	return data
}

func (s ScrollService) GetScrollConfigRawYaml() string {
	path := s.processCwd + "/.scroll_config.yml"

	content, err := os.ReadFile(path)

	if err != nil {
		return ""
	}

	return string(content)
}
