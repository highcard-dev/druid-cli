package domain

import (
	"fmt"
	"io"
	"os"

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

type File struct {
	Name       string                       `yaml:"name" json:"name"`
	Desc       string                       `yaml:"desc" json:"desc"`
	Version    *semver.Version              `yaml:"version" json:"version"`
	AppVersion string                       `yaml:"app_version" json:"app_version"` //don't make this a semver, it's not allways
	Init       string                       `yaml:"init" json:"init"`
	Processes  map[string]*ProcessCommand   `yaml:"processes" json:"processes"`
	Plugins    map[string]map[string]string `yaml:"plugins" json:"plugins"`
} // @name ScrollFile

type Scroll struct {
	File
} // @name Scroll

type Procedure struct {
	Mode string      `yaml:"mode"`
	Wait interface{} `yaml:"wait"`
	Data interface{} `yaml:"data"`
} // @name Procedure

// exec-tty, exec, stdin, command, scroll-switch
func (p *Procedure) IsInternalMode() bool {
	if p.Mode == "exec-tty" || p.Mode == "exec" || p.Mode == "stdin" || p.Mode == "command" || p.Mode == "scroll-switch" {
		return true
	}
	return false
}

type CommandInstructionSet struct {
	Procedures []*Procedure `yaml:"procedures" json:"procedures"`
	Needs      []string     `yaml:"needs,omitempty" json:"needs,omitempty"`
	Run        RunMode      `yaml:"run,omitempty" json:"run,omitempty"`
} // @name CommandInstructionSet

type ProcessCommand struct {
	Commands map[string]CommandInstructionSet `yaml:"commands" json:"commands"`
} // @name ProcessCommand

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
