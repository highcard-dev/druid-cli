package plugin

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/highcard-dev/daemon/plugin/proto"
	"google.golang.org/grpc"
)

// DruidDaemon is the interface that we're exposing as a plugin.
type DruidDaemon interface {
	NotifyConsole(string, string) error
}

// DruidPluginInterface is the interface that we're exposing as a plugin.
type DruidPluginInterface interface {
	GetModes() ([]*proto.GetModeResponse_Mode, error)
	RunProcedure(string, string) (string, error)
	Init(map[string]string, DruidDaemon, string, string) error
}

type DruidRpcPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	Impl DruidPluginInterface
}

func (p *DruidRpcPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterPluginServer(s, &GRPCServer{
		Impl:   p.Impl,
		broker: broker,
	})
	return nil
}

func (p *DruidRpcPlugin) GRPCClient(_ context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &GRPCClient{
		client: proto.NewPluginClient(c),
		broker: broker,
	}, nil
}

var _ plugin.GRPCPlugin = &DruidRpcPlugin{}
