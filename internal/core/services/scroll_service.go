package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	logger "github.com/highcard-dev/daemon/internal/core/services/log"
	"github.com/highcard-dev/daemon/internal/core/services/registry"
	"github.com/highcard-dev/daemon/internal/utils"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type ScrollService struct {
	pluginManager    *PluginManager
	logManager       ports.LogManagerInterface
	processManager   ports.ProcessManagerInterface
	hub              *WebsocketBroadcaster
	processCwd       string
	lock             *domain.ScrollLock
	scroll           *domain.Scroll
	ociRegistry      *registry.OciClient
	templateRenderer ports.TemplateRendererInterface
}
type TemplateData struct {
	Config interface{}
}

func NewScrollService(
	processCwd string,
	ociRegistry *registry.OciClient,
	logManager ports.LogManagerInterface,
	processManager ports.ProcessManagerInterface,
	hub *WebsocketBroadcaster,
	pluginManager *PluginManager,
) *ScrollService {
	s := &ScrollService{
		processCwd:       processCwd,
		processManager:   processManager,
		logManager:       logManager,
		hub:              hub,
		ociRegistry:      ociRegistry,
		pluginManager:    pluginManager,
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

	if err != nil {
		return nil, err
	}

	go sc.hub.run()

	go func() {
		for {
			select {
			case item := <-sc.pluginManager.NotifyConsole:
				cmd := domain.StreamCommand{
					Data:   item.Data,
					Stream: item.Stream,
				}
				sc.logManager.AddLine(item.Stream, cmd)
				encoded, _ := json.Marshal(cmd)
				sc.hub.broadcast <- encoded
			}
		}
	}()

	//init plugins
	err = sc.pluginManager.ParseFromScroll(scroll.Plugins, sc.GetScrollConfigRawYaml(), sc.processCwd)

	return scroll, err
}

func (sc *ScrollService) StartLockfile() error {
	for process, status := range sc.lock.Statuses {
		if status != "start" {
			continue
		}
		for cmdName, cmd := range sc.scroll.Processes[process].Commands {
			if cmd.SchouldChangeStatus == "start" {
				logger.Log().Info("Running command",
					zap.String("commandName", cmdName),
				)
				go sc.Run(cmdName, process, true)
			}
		}
	}
	return nil
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

					return ioutil.WriteFile(realPath, b, 0644)
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

func (sc *ScrollService) changeLockStatus(process string, status string) error {
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

func (sc *ScrollService) Run(cmd string, processId string, changeStatus bool) error {
	logger.Log().LogRunCommand(processId, cmd)

	var command domain.CommandInstructionSet

	//check if we can accually do it before we start
	if ps, ok := sc.scroll.Processes[processId]; ok {
		cmds, ok := ps.Commands[cmd]
		if !ok {
			return errors.New("command " + cmd + " not found")
		}
		command = cmds
	} else {
		return errors.New("process " + processId + " not found")
	}

	if changeStatus && command.SchouldChangeStatus != "" {
		sc.changeLockStatus(processId, command.SchouldChangeStatus)
	}
	for k, proc := range command.Procedures {
		logger.Log().LogRunProcedure(processId, cmd, k)
		_, err := sc.RunProcedure(proc, processId, changeStatus)
		if err != nil {
			return err
		}
	}

	return nil
}

func (sc *ScrollService) RunProcedure(proc *domain.Procedure, processId string, changeStatus bool) (string, error) {

	//check if we have a plugin for the mode
	if sc.pluginManager.HasMode(proc.Mode) {

		val, ok := proc.Data.(string)
		if !ok {
			return "", fmt.Errorf("invalid data type for plugin mode %s, expected data to be string but go %v", proc.Mode, proc.Data)
		}

		res, err := sc.pluginManager.RunProcedure(proc.Mode, val)
		return res, err
	}
	//check internal
	switch proc.Mode {
	//exec = create new process
	case "exec":
		//TODO: Mutex

		logger.Log().Debug("Checking for running process",
			zap.String("processId", processId),
		)

		process, _ := sc.GetRunningProcess(processId)
		if process != nil {
			return "", errors.New("process already running")
		}
		newProcess := &domain.Process{Name: processId}
		sc.processManager.GetRunningProcesses()[processId] = newProcess
		instructionsRaw := proc.Data.([]interface{})

		// :((((
		// we have to manually []interface{} to []string
		instructions := make([]string, len(instructionsRaw))
		for i, v := range instructionsRaw {
			val, ok := v.(string)
			if !ok {
				return "", errors.New("invalid instruction, cannot convert to string")
			}
			instructions[i] = val
		}

		logger.Log().Debug("Launching exec process",
			zap.String("processId", processId),
			zap.String("cwd", sc.processCwd),
			zap.Strings("instructions", instructions),
		)
		err := sc.processManager.Launch(newProcess, instructions, sc.processCwd)
		if err != nil {
			return "", err
		}
		delete(sc.processManager.GetRunningProcesses(), processId)
	case "stdin":

		logger.Log().Debug("Launching stdin process",
			zap.String("processId", processId),
			zap.String("cwd", sc.processCwd),
			zap.String("instructions", proc.Data.(string)),
		)

		process, err := sc.GetRunningProcess(processId)
		if err != nil {
			return "", err
		}
		sc.processManager.WriteStdin(process, proc.Data.(string))

	case "command":

		logger.Log().Debug("Launching stdin process",
			zap.String("processId", processId),
			zap.String("cwd", sc.processCwd),
			zap.String("instructions", proc.Data.(string)),
		)

		err := sc.Run(proc.Data.(string), processId, changeStatus)
		return "", err

	case "scroll-switch":

		logger.Log().Debug("Launching scroll-switch process",
			zap.String("processId", processId),
			zap.String("cwd", sc.processCwd),
			zap.String("instructions", proc.Data.(string)),
		)

		err := sc.ociRegistry.Pull(sc.GetDir(), proc.Data.(string))
		return "", err
	default:
		return "", errors.New("Unknown mode " + proc.Mode)
	}
	return "", nil
}

func (sc *ScrollService) GetDir() string {
	return utils.GetScrollDirFromCwd(sc.processCwd)
}

func (sc *ScrollService) GetRunningProcess(name string) (*domain.Process, error) {
	if process, ok := sc.processManager.GetRunningProcesses()[name]; ok {
		return process, nil
	}
	return nil, errors.New("process not found")
}

func (sc *ScrollService) GetRunningProcesses() map[string]*domain.Process {
	return sc.processManager.GetRunningProcesses()
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

func (s ScrollService) Initalize() error {

	parts := strings.Split(s.scroll.Init, ".")

	if len(parts) != 2 {
		return errors.New("invalid init command")
	}
	initCommands := s.scroll.Processes[parts[0]].Commands[parts[1]].Procedures

	if len(initCommands) > 0 {
		go s.Run(parts[1], parts[0], true)
		s.lock.Initialized = true
		s.lock.Write()
	}
	return nil
}

func (s ScrollService) RenderCwdTemplates() error {
	cwd := s.processCwd

	libRegEx, err := regexp.Compile("^.+\\.(scroll_template)$")
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

	content, err := ioutil.ReadFile(path)

	if err != nil {
		return data
	}

	// Unmarshal the YAML data into the struct
	yaml.Unmarshal(content, &data)

	return data
}

func (s ScrollService) GetScrollConfigRawYaml() string {
	path := s.processCwd + "/.scroll_config.yml"

	content, err := ioutil.ReadFile(path)

	if err != nil {
		return ""
	}

	return string(content)
}
