package main

import (
	"errors"
	"io"
	"time"

	"log"

	goplugin "github.com/hashicorp/go-plugin"
	plugin "github.com/highcard-dev/daemon/plugin"
	"github.com/highcard-dev/daemon/plugin/proto"
	rconLib "github.com/highcard-dev/gorcon"
)

type ScrollConfig struct {
	Password       string `yaml:"password"`
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	ConnectionMode string `yaml:"connectionMode"` // constant or short, short is e.g. for minecraft wich wants  connections and then disconnects
}

// Here is a real implementation of Rcon
type DruidPluginImpl struct {
	conn           *rconLib.Conn
	environment    *plugin.Environment
	config         map[string]string
	connectionMode string
	mainClient     plugin.DruidDaemon
}

func main() {

	log.Println("Starting RCON Plugin")

	rcon := &DruidPluginImpl{}
	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goplugin.Plugin{
		"rcon": &plugin.DruidRpcPlugin{Impl: rcon},
	}

	log.Println("RCON Plugin started")

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      goplugin.DefaultGRPCServer,
	})
}

func (g *DruidPluginImpl) ensureConnection(silent bool) error {
	log.Println("Connecting to " + g.environment.Address)
	if g.conn != nil {
		return nil
	}
	conn, err := rconLib.Dial(g.environment.Address, g.environment.Password)
	if err != nil {
		if !silent {
			log.Printf("Error connecting to RCON server: %s", err.Error())
		}
		return err
	}

	err = g.mainClient.NotifyConsole("rcon", "Connected to RCON server")
	if err != nil {
		log.Printf("Error notifying console: %s", err.Error())
	}
	log.Println("Connected to RCON server")
	g.conn = conn
	return nil
}

func (g *DruidPluginImpl) GetModes() ([]*proto.GetModeResponse_Mode, error) {
	rcon := proto.GetModeResponse_Mode{Mode: "rcon", Standalone: true}
	return []*proto.GetModeResponse_Mode{&rcon}, nil
}

func (g *DruidPluginImpl) runProcedureConstant(key string, value string) (string, error) {
	g.ensureConnection(false)
	if g.conn == nil {
		log.Println("RCON connection not established")
		return "", errors.New("RCON connection not established")

	}
	response, err := g.conn.Execute(value)
	if err != nil {
		log.Println(err.Error())
		g.conn.Close()
		g.conn = nil
		g.ensureConnection(false)
		response, err = g.conn.Execute(value)
	}
	return response, err
}

func (g *DruidPluginImpl) runProcedureShort(key string, value string) (string, error) {

	conn, err := rconLib.Dial(g.environment.Address, g.environment.Password)
	if err != nil {
		log.Println(err.Error())
		return "", errors.New(err.Error())

	}
	defer conn.Close()
	log.Println("Connected to RCON server")
	response, err := conn.Execute(value)
	if err != nil {
		println(err.Error())
		err = g.mainClient.NotifyConsole("rcon", "Rcon Error: "+err.Error())
		return "", err
	}
	err = g.mainClient.NotifyConsole("rcon", response)
	return response, err
}
func (g *DruidPluginImpl) RunProcedure(key string, value string) (string, error) {
	if g.connectionMode == "constant" {
		return g.runProcedureConstant(key, value)
	} else if g.connectionMode == "short" {
		return g.runProcedureShort(key, value)
	} else {
		return "", errors.New("unknown connection mode")
	}
}

func (g *DruidPluginImpl) Init(config map[string]string, client plugin.DruidDaemon, cwd string, scrollConfigRawYaml string) error {

	scrollConfig, err := plugin.GetConfig[ScrollConfig]("rcon", []byte(scrollConfigRawYaml))

	if err != nil {
		return err
	}

	host := scrollConfig.Host
	port := scrollConfig.Port
	password := scrollConfig.Password

	log.Printf("Initializing RCON Plugin with config: %v, cwd: %s", config, cwd)

	g.mainClient = client
	g.config = config

	environment, err := plugin.NewPluginEnvironment(cwd, password, port, host)
	if err != nil {
		log.Printf("Error creating environment: %s", err.Error())
		return err
	}
	g.environment = environment

	if scrollConfig.ConnectionMode == "" {
		g.connectionMode = "short"
		err = g.mainClient.NotifyConsole("rcon", "Connection mode not set, defaulting to short\n")
		if err != nil {
			return err
		}
	} else {
		g.connectionMode = scrollConfig.ConnectionMode
	}

	log.Printf("Connection mode: %s", g.connectionMode)

	if g.connectionMode == "constant" {
		go func() {
			for {
				if g.conn == nil {
					log.Println("RCON connection not established, trying to connect")

					time.Sleep(time.Second)

					g.ensureConnection(true)
					continue
				}
				packet, err := g.conn.Read()
				if err != nil {
					if err == io.EOF {
						log.Println("RCON connection closed")
						g.conn = nil
					}
					continue
				}
				err = g.mainClient.NotifyConsole("rcon", packet.Body())
				if err != nil {
					log.Printf("Error notifying console: %s", err.Error())
				}
			}
		}()
	}

	log.Println("RCON Plugin initialized")

	return nil
}

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
// This prevents users from executing bad plugins or executing a plugin
// directory. It is a UX feature, not a security feature.
var handshakeConfig = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "DRUID_PLUGIN",
	MagicCookieValue: "druid_is_the_way",
}
