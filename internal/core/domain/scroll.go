package domain

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type RunMode string

const (
	RunModeAlways     RunMode = "always"     //default
	RunModeOnce       RunMode = "once"       //runs only once
	RunModeRestart    RunMode = "restart"    //restarts on failure
	RunModePersistent RunMode = "persistent" //restarts on failure and on program restart
)

type Chunks struct {
	Name       string    `yaml:"name"`
	Path       string    `yaml:"path"`
	SkipUpdate bool      `yaml:"skip_update,omitempty" json:"skip_update,omitempty"`
	Chunks     []*Chunks `yaml:"chunks,omitempty" json:"chunks,omitempty"`
}

type Port struct {
	Port        int    `yaml:"port" json:"port"`
	Protocol    string `yaml:"protocol" json:"protocol"`
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

type AugmentedPort struct {
	Port
	ColdstarterHandler string            `json:"-"`
	ColdstarterVars    map[string]string `json:"-"`
	InactiveSince      time.Time         `json:"inactive_since"`
	InactiveSinceSec   uint              `json:"inactive_since_sec"`
	Open               bool              `json:"open"`
}

type File struct {
	Name        string                            `yaml:"name" json:"name"`
	Desc        string                            `yaml:"desc" json:"desc"`
	PullChannel map[string]string                 `yaml:"pull_channel" json:"pull_channel"`
	Version     *semver.Version                   `yaml:"version" json:"version"`
	AppVersion  string                            `yaml:"app_version" json:"app_version"` //don't make this a semver, it's not allways
	Serve       string                            `yaml:"serve" json:"serve"`
	Ports       []Port                            `yaml:"ports" json:"ports"`
	Commands    map[string]*CommandInstructionSet `yaml:"commands" json:"commands"`
	Chunks      []*Chunks                         `yaml:"chunks" json:"chunks"`
}

type Scroll struct {
	File
	scrollDir string
}

type ProcedureType string

const (
	ProcedureTypeContainer ProcedureType = "container"
	ProcedureTypeSignal    ProcedureType = "signal"
)

type Procedure struct {
	Type          ProcedureType     `yaml:"type,omitempty" json:"type,omitempty"`
	Id            *string           `yaml:"id,omitempty" json:"id,omitempty"`
	IgnoreFailure bool              `yaml:"ignore_failure" json:"ignore_failure"`
	Image         string            `yaml:"image,omitempty" json:"image,omitempty"`
	Command       []string          `yaml:"command,omitempty" json:"command,omitempty"`
	WorkingDir    string            `yaml:"working_dir,omitempty" json:"working_dir,omitempty"`
	Env           map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	ExpectedPorts []ExpectedPort    `yaml:"expectedPorts,omitempty" json:"expectedPorts,omitempty"`
	Mounts        []Mount           `yaml:"mounts,omitempty" json:"mounts,omitempty"`
	Target        string            `yaml:"target,omitempty" json:"target,omitempty"`
	Signal        string            `yaml:"signal,omitempty" json:"signal,omitempty"`
	TTY           bool              `yaml:"tty,omitempty" json:"tty,omitempty"`

	Mode string      `yaml:"mode,omitempty" json:"-"`
	Wait interface{} `yaml:"wait,omitempty" json:"-"`
	Data interface{} `yaml:"data,omitempty" json:"-"`
}

func (p *Procedure) Kind() ProcedureType {
	if p.Type == "" {
		return ProcedureTypeContainer
	}
	return p.Type
}

func (p *Procedure) IsContainer() bool {
	return p.Kind() == ProcedureTypeContainer
}

func (p *Procedure) IsSignal() bool {
	return p.Kind() == ProcedureTypeSignal
}

func ProcedureName(commandName string, idx int, procedure *Procedure) string {
	name := fmt.Sprintf("%s.%d", commandName, idx)
	if procedure != nil && procedure.Id != nil {
		name = *procedure.Id
	}
	return name
}

func (p *Procedure) hasContainerFields() bool {
	return p.Image != "" ||
		len(p.Command) > 0 ||
		p.WorkingDir != "" ||
		len(p.Env) > 0 ||
		len(p.ExpectedPorts) > 0 ||
		len(p.Mounts) > 0 ||
		p.TTY
}

type Mount struct {
	Path     string `yaml:"path" json:"path"`
	SubPath  string `yaml:"sub_path,omitempty" json:"sub_path,omitempty"`
	ReadOnly bool   `yaml:"read_only,omitempty" json:"read_only,omitempty"`
}

type ExpectedPort struct {
	Name             string `yaml:"name" json:"name"`
	KeepAliveTraffic string `yaml:"keepAliveTraffic,omitempty" json:"keepAliveTraffic,omitempty"`
}

type TrafficThreshold struct {
	Bytes  uint64
	Window time.Duration
}

type RuntimePortStatus struct {
	Name             string     `json:"name"`
	Procedure        string     `json:"procedure"`
	Port             int        `json:"port"`
	Protocol         string     `json:"protocol"`
	Bound            bool       `json:"bound"`
	HostIP           string     `json:"host_ip,omitempty"`
	HostPort         int        `json:"host_port,omitempty"`
	Traffic          bool       `json:"traffic"`
	TrafficBytes     *uint64    `json:"traffic_bytes,omitempty"`
	RXBytes          *uint64    `json:"rx_bytes,omitempty"`
	TXBytes          *uint64    `json:"tx_bytes,omitempty"`
	KeepAliveTraffic string     `json:"keepAliveTraffic,omitempty"`
	TrafficWindow    string     `json:"traffic_window,omitempty"`
	TrafficOK        *bool      `json:"traffic_ok,omitempty"`
	LastActivityAt   *time.Time `json:"last_activity_at,omitempty"`
	Source           string     `json:"source"`
}

var trafficThresholdPattern = regexp.MustCompile(`(?i)^([0-9]+)(b|kb|mb|gb)/(.+)$`)

func ParseKeepAliveTraffic(value string) (*TrafficThreshold, error) {
	if value == "" {
		return nil, nil
	}
	matches := trafficThresholdPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(matches) != 4 {
		return nil, fmt.Errorf("invalid keepAliveTraffic %q, expected format like 10kb/5m", value)
	}
	amount, err := strconv.ParseUint(matches[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid keepAliveTraffic amount %q: %w", matches[1], err)
	}
	switch strings.ToLower(matches[2]) {
	case "kb":
		amount *= 1000
	case "mb":
		amount *= 1000 * 1000
	case "gb":
		amount *= 1000 * 1000 * 1000
	}
	window, err := time.ParseDuration(matches[3])
	if err != nil || window <= 0 {
		return nil, fmt.Errorf("invalid keepAliveTraffic window %q", matches[3])
	}
	return &TrafficThreshold{Bytes: amount, Window: window}, nil
}

type CommandInstructionSet struct {
	Procedures []*Procedure `yaml:"procedures" json:"procedures"`
	Needs      []string     `yaml:"needs,omitempty" json:"needs,omitempty"`
	Run        RunMode      `yaml:"run,omitempty" json:"run,omitempty"`
}

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
	scroll := Scroll{
		scrollDir: scrollDir,
	}
	if _, err = scroll.ParseFile(file); err != nil {
		return nil, err
	}
	return &scroll, nil
}

func NewScrollFromBytes(scrollDir string, file []byte) (*Scroll, error) {
	scroll := Scroll{
		scrollDir: scrollDir,
	}
	if _, err := scroll.ParseFile(file); err != nil {
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

func (sc *Scroll) Validate(strict bool) error {
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
	if len(sc.Commands) == 0 {
		return fmt.Errorf("scroll commands are required")
	}
	if sc.Serve != "" {
		if _, ok := sc.Commands[sc.Serve]; !ok {
			return fmt.Errorf("scroll serve command %s is not defined", sc.Serve)
		}
	}

	ids := make(map[string]bool)
	portsByName := make(map[string]bool, len(sc.Ports))
	for _, port := range sc.Ports {
		if port.Name != "" {
			portsByName[port.Name] = true
		}
	}
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
			if p == nil {
				return fmt.Errorf("procedure is required")
			}
			if p.Mode != "" {
				return fmt.Errorf("procedure field mode is unsupported; use type: container or type: signal")
			}
			if p.Wait != nil {
				return fmt.Errorf("procedure field wait is unsupported")
			}
			if p.Data != nil {
				return fmt.Errorf("procedure field data is unsupported; use container command fields or type: signal")
			}
			switch p.Kind() {
			case ProcedureTypeContainer:
				if p.Image == "" {
					return fmt.Errorf("container procedure image is required")
				}
				if p.Target != "" || p.Signal != "" {
					return fmt.Errorf("container procedure cannot set target or signal; use type: signal")
				}
				mountPaths := map[string]bool{}
				for _, mount := range p.Mounts {
					if mount.Path == "" {
						return fmt.Errorf("mount path is required")
					}
					if !filepath.IsAbs(mount.Path) {
						return fmt.Errorf("mount path %s must be absolute", mount.Path)
					}
					if mountPaths[mount.Path] {
						return fmt.Errorf("mount path %s is duplicated", mount.Path)
					}
					mountPaths[mount.Path] = true
					if mount.SubPath == "" {
						continue
					}
					if filepath.IsAbs(mount.SubPath) {
						return fmt.Errorf("mount sub_path %s must be relative", mount.SubPath)
					}
					clean := filepath.Clean(mount.SubPath)
					if clean == ".." || strings.HasPrefix(clean, "../") {
						return fmt.Errorf("mount sub_path %s escapes runtime root", mount.SubPath)
					}
				}
				for _, expectedPort := range p.ExpectedPorts {
					if expectedPort.Name == "" {
						return fmt.Errorf("expected port name is required")
					}
					if !portsByName[expectedPort.Name] {
						return fmt.Errorf("expected port %s is not defined in top-level ports", expectedPort.Name)
					}
					if _, err := ParseKeepAliveTraffic(expectedPort.KeepAliveTraffic); err != nil {
						return err
					}
				}
			case ProcedureTypeSignal:
				if p.Target == "" {
					return fmt.Errorf("signal procedure target is required")
				}
				if p.Signal == "" {
					return fmt.Errorf("signal procedure signal is required")
				}
				if p.hasContainerFields() {
					return fmt.Errorf("signal procedure cannot set container fields")
				}
			default:
				return fmt.Errorf("unsupported procedure type %q", p.Type)
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
	//scan for files in sc.scrollDir
	if sc.scrollDir == "" {
		return nil
	}
	entries, err := os.ReadDir(sc.scrollDir)
	if err != nil {
		if !strict && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read scroll directory - %w", err)
	}
	for _, entry := range entries {
		var found = false
		for fileName := range ScrollFiles {
			if entry.Name() == fileName {
				found = true
				break
			}
		}
		if !found {
			if !strict {
				logger.Log().Warn("Directory contains file that is not defined in ScrollFiles", zap.String("file", entry.Name()))
			} else {
				return fmt.Errorf("directory contains file that is not defined in ScrollFiles: %s", entry.Name())
			}
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

const ScrollDataDir = "data"

// DataLoadedMarkerFile is created under the scroll data directory after a successful
// registry pull of scroll-data layers (e.g. from coldstarter OnBeforeFinish).
const DataLoadedMarkerFile = ".data-loaded"

var ScrollFiles = map[string]ArtifactType{
	"update":                            ArtifactTypeScrollFs,
	"scroll.yaml":                       ArtifactTypeScrollFs,
	"public":                            ArtifactTypeScrollFs,
	"private":                           ArtifactTypeScrollFs,
	"packet_handler":                    ArtifactTypeScrollFs,
	"scroll-config.yml.scroll_template": ArtifactTypeScrollFs,
	"data":                              ArtifactTypeScrollData,
	".meta":                             ArtifactTypeScrollFs,
}
