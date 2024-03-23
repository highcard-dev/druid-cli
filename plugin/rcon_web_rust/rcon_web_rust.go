package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	plugins "github.com/highcard-dev/daemon/plugin"
	"github.com/highcard-dev/daemon/plugin/proto"

	"github.com/gorilla/websocket"
)

type ScrollConfig struct {
	Password string `yaml:"password"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
}

type Message struct {
	Identifier int32
	Message    string
	Name       string
}

type Response struct {
	Identifier int32
	Message    string
	Type       string
	Stacktrace string
}

type DruidPluginImpl struct {
	conn        *websocket.Conn
	config      map[string]string
	environment *plugins.Environment
	mainClient  plugins.DruidDaemon
	procedures  map[int32]chan *Response
}

func (g *DruidPluginImpl) ensureConnection() error {
	if g.conn != nil {
		return nil
	}
	u := url.URL{Scheme: "ws", Host: g.environment.Address, Path: "/" + g.environment.Password}
	log.Println("Connecting to " + u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)

	if err != nil {
		logger.Log().Error(err.Error())
		return err
	}
	g.mainClient.NotifyConsole("rcon_web_rust", "Connected to WebRCON")
	log.Println("Connected to " + u.String())
	g.conn = c
	return nil
}
func (g *DruidPluginImpl) GetModes() ([]*proto.GetModeResponse_Mode, error) {
	rcon := proto.GetModeResponse_Mode{Mode: "rcon_web_rust", Standalone: true}
	return []*proto.GetModeResponse_Mode{&rcon}, nil
}

func (g *DruidPluginImpl) RunProcedure(key string, value string) (string, error) {
	randId := int32(rand.Int())
	err := g.ensureConnection()
	if err != nil {
		logger.Log().Error(fmt.Sprintf("RCON Web connection not established: %s", err.Error()))
		return "", errors.New("RCON Web connection not established")

	}
	g.procedures[randId] = make(chan *Response)
	m := Message{
		Identifier: randId,
		Message:    value,
		Name:       "WebRcon",
	}
	g.conn.WriteJSON(m)
	var message *Response
loop:
	for timeout := time.After(5 * time.Second); ; {
		select {
		case <-timeout:
			return "", errors.New("execute timeout")
		case m := <-g.procedures[randId]:
			if m.Identifier == randId {
				message = m
				break loop
			}
		}
	}

	delete(g.procedures, randId)
	return message.Message, err
}

func (g *DruidPluginImpl) Init(config map[string]string, client plugins.DruidDaemon, cwd string, scrollConfigRawYaml string) error {

	log.Println(scrollConfigRawYaml)

	scrollConfig, err := plugins.GetConfig[ScrollConfig]("rcon_web_rust", []byte(scrollConfigRawYaml))

	g.mainClient = client
	g.config = config
	g.procedures = make(map[int32]chan *Response)

	host := scrollConfig.Host
	port := scrollConfig.Port
	password := scrollConfig.Password

	environment, err := plugins.NewPluginEnvironment(cwd, password, port, host)
	if err != nil {
		return err
	}
	g.environment = environment
	g.ensureConnection()
	go func() {
		for {
			if g.conn == nil {
				log.Println("Trying to reconnect to Web RCON server")
				g.ensureConnection()
				time.Sleep(time.Second)

				continue
			}
			_, m, err := g.conn.ReadMessage()
			if err != nil {
				log.Println("Web RCON connection closed")
				g.conn.Close()
				g.conn = nil
				continue
			}
			g.mainClient.NotifyConsole("rcon_web_rust", string(m))
			var r Response
			err = json.Unmarshal([]byte(m), &r)
			if err != nil {
				continue
			}

			if ch, ok := g.procedures[r.Identifier]; ok {
				go func() {
					ch <- &r
				}()
			}
		}
	}()

	log.Println("Web RCON Plugin initialized")

	return nil
}

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
// This prevents users from executing bad plugins or executing a plugin
// directory. It is a UX feature, not a security feature.
var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "DRUID_PLUGIN",
	MagicCookieValue: "druid_is_the_way",
}

func main() {
	rcon := &DruidPluginImpl{}
	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]plugin.Plugin{
		"rcon_web_rust": &plugins.DruidRpcPlugin{Impl: rcon},
	}

	log.Println("RCON Web Plugin started")
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}
