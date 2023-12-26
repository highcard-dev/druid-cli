package services

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/highcard-dev/logger"
	commons "github.com/highcard-dev/plugin"

	"github.com/hashicorp/go-plugin"
)

type NotifcationHandler struct {
	broadcast chan StreamItem
}
type StreamItem struct {
	Data   string
	Stream string
}

func (n *NotifcationHandler) NotifyConsole(mode string, data string) error {
	go func() {

		item := StreamItem{
			Stream: mode,
			Data:   data,
		}
		n.broadcast <- item
	}()

	return nil
}

type PluginManager struct {
	Modes                  map[string]string
	plugins                map[string]commons.DruidPluginInterface
	allowedStandaloneModes []string
	NotifyConsole          chan StreamItem
}

func NewPluginManager() *PluginManager {
	return &PluginManager{
		Modes:         make(map[string]string),
		plugins:       make(map[string]commons.DruidPluginInterface),
		NotifyConsole: make(chan StreamItem),
	}
}

func (pm *PluginManager) ParseFromScroll(pluginDefinitionMap map[string]map[string]string, config string, cwd string) error {
	for pluginName, pluginDefinition := range pluginDefinitionMap {
		p, err := pm.LoadGoPlugin(pluginName)
		if err != nil {
			return err
		}
		n := NotifcationHandler{
			broadcast: pm.NotifyConsole,
		}
		err = p.Init(pluginDefinition, &n, cwd, config)
		if err != nil {
			return fmt.Errorf("error initializing plugin %s: %s", pluginName, err.Error())
		}
		modes, _ := p.GetModes()
		pm.plugins[pluginName] = p
		for _, mode := range modes {
			if mode.Standalone {
				pm.AddStandaloneMode(mode.Mode)
			}
			pm.Modes[mode.Mode] = pluginName
		}
	}

	return nil
}

func (pm *PluginManager) HasMode(mode string) bool {
	if _, ok := pm.Modes[mode]; ok {
		return true
	}
	return false
}

func (pm *PluginManager) RunProcedure(mode string, value string) (string, error) {
	if pm.HasMode(mode) {
		return pm.plugins[pm.Modes[mode]].RunProcedure(mode, value)
	} else {
		return "", errors.New("mode not suported by any plugin")
	}
}

func (pm *PluginManager) LoadGoPlugin(name string) (commons.DruidPluginInterface, error) {

	var handshakeConfig = plugin.HandshakeConfig{
		ProtocolVersion:  1,
		MagicCookieKey:   "DRUID_PLUGIN",
		MagicCookieValue: "druid_is_the_way",
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]plugin.Plugin{
		"rcon":          &commons.DruidRpcPlugin{},
		"rcon_web_rust": &commons.DruidRpcPlugin{},
	}

	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}
	exPath := filepath.Dir(ex)
	var path string
	_, err = os.Stat(exPath + "/druid_" + name)
	if err == nil {
		path = exPath + "/druid_" + name
	} else {
		path = "./druid_" + name
	}

	var cmd *exec.Cmd

	if os.Getenv("DRUID_DEBUG_PATH") != "" {
		cmd = exec.Command("/bin/sh", os.Getenv("DRUID_DEBUG_PATH"), path)
	} else {
		cmd = exec.Command(path)
	}
	// This doesn't add more security than before
	// but removes the SecureConfig is nil warning.
	pluginChecksum, err := getPluginExecutableChecksum(path)
	if err != nil {
		return nil, fmt.Errorf("unable to generate a checksum for the plugin %s", path)
	}

	// We're a host! Start by launching the plugin process.
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		Cmd:             cmd,
		Logger:          logger.Hclog2ZapLogger{Zap: logger.Log()},
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolNetRPC, plugin.ProtocolGRPC},
		SecureConfig: &plugin.SecureConfig{
			Checksum: pluginChecksum,
			Hash:     sha256.New(),
		},
	})
	//defer client.Kill()

	// Connect via RPC
	rpcClient, err := client.Client()
	if err != nil {
		return nil, err
	}

	// Request the plugin
	raw, err := rpcClient.Dispense(name)
	if err != nil {
		return nil, err
	}
	rpcConnection := raw.(commons.DruidPluginInterface)

	return rpcConnection, nil
}

func (pm *PluginManager) CanRunStandaloneProcedure(mode string) bool {
	for _, standaloneMode := range pm.allowedStandaloneModes {
		if standaloneMode == mode {
			return true
		}
	}
	return false
}

func (pm *PluginManager) AddStandaloneMode(mode string) {
	pm.allowedStandaloneModes = append(pm.allowedStandaloneModes, mode)
}
func getPluginExecutableChecksum(executablePath string) ([]byte, error) {
	pathHash := sha256.New()
	file, err := os.Open(executablePath)

	if err != nil {
		return nil, err
	}

	defer file.Close()

	_, err = io.Copy(pathHash, file)
	if err != nil {
		return nil, err
	}

	return pathHash.Sum(nil), nil
}
