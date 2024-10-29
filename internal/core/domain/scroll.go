package domain

import (
	"fmt"
	"io"
	"os"
	"time"

	semver "github.com/Masterminds/semver/v3"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type RunMode string

const (
	RunModeAlways  RunMode = "always" //default
	RunModeOnce    RunMode = "once"
	RunModeRestart RunMode = "restart"
)

type Cronjob struct {
	Name     string `yaml:"name"`
	Schedule string `yaml:"schedule"`
	Command  string `yaml:"command"`
}

type ColdStarterVars struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

type Port struct {
	Port         int               `yaml:"port" json:"port"`
	Protocol     string            `yaml:"protocol" json:"protocol"`
	Name         string            `yaml:"name" json:"name"`
	SleepHandler *string           `yaml:"sleep_handler" json:"sleep_handler"`
	Mandatory    bool              `yaml:"mandatory" json:"mandatory"`
	Vars         []ColdStarterVars `yaml:"vars" json:"vars"`
	StartDelay   uint              `yaml:"start_delay" json:"start_delay"`
}

type AugmentedPort struct {
	Port
	InactiveSince    time.Time `json:"inactive_since"`
	InactiveSinceSec uint      `json:"inactive_since_sec"`
	Open             bool      `json:"open"`
}

type File struct {
	Name       string                            `yaml:"name" json:"name"`
	Desc       string                            `yaml:"desc" json:"desc"`
	Version    *semver.Version                   `yaml:"version" json:"version"`
	AppVersion string                            `yaml:"app_version" json:"app_version"` //don't make this a semver, it's not allways
	Init       string                            `yaml:"init" json:"init"`
	Ports      []Port                            `yaml:"ports" json:"ports"`
	Commands   map[string]*CommandInstructionSet `yaml:"commands" json:"commands"`
	Plugins    map[string]map[string]string      `yaml:"plugins" json:"plugins"`
	Cronjobs   []*Cronjob                        `yaml:"cronjobs" json:"cronjobs"`
} // @name ScrollFile

type Scroll struct {
	File
} // @name Scroll

type Procedure struct {
	Mode string      `yaml:"mode"`
	Id   *string     `yaml:"id"`
	Wait interface{} `yaml:"wait"`
	Data interface{} `yaml:"data"`
} // @name Procedure

type CommandInstructionSet struct {
	Procedures []*Procedure `yaml:"procedures" json:"procedures"`
	Needs      []string     `yaml:"needs,omitempty" json:"needs,omitempty"`
	Run        RunMode      `yaml:"run,omitempty" json:"run,omitempty"`
} // @name CommandInstructionSet

var ErrScrollDoesNotExist = fmt.Errorf("scroll does not exist")

func NewScroll(scrollDir string) (*Scroll, error) {

	filePath := scrollDir + "/scroll.yaml"

	yamlFile, err := os.Open(filePath)
	// if we os.Open returns an error then handle it
	if err != nil {
		if os.IsNotExist(err) {
			logger.Log().Warn("scroll.yaml does not exist", zap.String("path", filePath))
			return nil, ErrScrollDoesNotExist
		}
		return nil, fmt.Errorf("failed to open scroll.yaml - %w", err)
	}
	defer yamlFile.Close()
	file, err := io.ReadAll(yamlFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read scroll.yaml - %w", err)
	}
	scroll := Scroll{}
	if _, err = scroll.ParseFile(file); err != nil {
		return nil, err
	}
	return &scroll, nil
}

func (sc *Scroll) ParseFile(file []byte) (*Scroll, error) {
	valueReplacedScroll := os.ExpandEnv(string(file))

	var f File
	err := yaml.Unmarshal([]byte(valueReplacedScroll), &f)
	if err != nil {
		return nil, err
	}

	sc.File = f
	return sc, nil
}

func (sc *Scroll) Validate() error {
	if sc.Name == "" {
		return fmt.Errorf("scroll name is required")
	}
	if sc.Desc == "" {
		return fmt.Errorf("scroll description is required")
	}
	if sc.Version == nil {
		return fmt.Errorf("scroll version is required")
	}
	if sc.AppVersion == "" {
		return fmt.Errorf("scroll app_version is required")
	}
	if sc.Init == "" {
		return fmt.Errorf("scroll init is required")
	}
	if len(sc.Commands) == 0 {
		return fmt.Errorf("scroll commands are required")
	}

	ids := make(map[string]bool)
	for cmd, cis := range sc.Commands {
		if cmd == "" {
			return fmt.Errorf("command name is required")
		}
		if cis == nil {
			return fmt.Errorf("command instruction set is required")
		}
		if len(cis.Procedures) == 0 {
			return fmt.Errorf("command procedures are required")
		}
		for _, p := range cis.Procedures {
			if p.Mode == "" {
				return fmt.Errorf("procedure mode is required")
			}
			if p.Id == nil {
				continue
			}
			if _, ok := ids[*p.Id]; ok {
				return fmt.Errorf("procedure id %s is not unique", *p.Id)
			}
			ids[*p.Id] = true
		}
	}
	return nil
}

func (sc *Scroll) CanColdStart() bool {
	return len(sc.Ports) != 0
}

func (sc *Scroll) GetColdStartPorts() []Port {
	return sc.Ports
}
