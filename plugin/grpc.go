package plugin

import (
	plugin "github.com/hashicorp/go-plugin"
	"github.com/highcard-dev/daemon/plugin/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// GRPCClient is an implementation of KV that talks over RPC.
type GRPCClient struct {
	broker *plugin.GRPCBroker
	client proto.PluginClient
}

func (m *GRPCClient) GetModes() ([]*proto.GetModeResponse_Mode, error) {
	resp, err := m.client.GetModes(context.Background(), &proto.EmptyPluginResponse{})
	if err != nil {
		return nil, err
	}

	return resp.Modes, nil
}

func (m *GRPCClient) RunProcedure(mode string, data string) (string, error) {
	resp, err := m.client.RunProcedure(context.Background(), &proto.ProcedureRequest{Mode: mode, Data: data})
	if err != nil {
		return "", err
	}

	return resp.Data, nil
}

func (m *GRPCClient) Init(pluginMap map[string]string, a DruidDaemon, cwd string, config string) error {
	addHelperServer := &GRPCAddHelperServer{Impl: a}

	var s *grpc.Server
	serverFunc := func(opts []grpc.ServerOption) *grpc.Server {
		s = grpc.NewServer(opts...)
		proto.RegisterDaemonServiceServer(s, addHelperServer)

		return s
	}

	brokerID := m.broker.NextId()
	go m.broker.AcceptAndServe(brokerID, serverFunc)

	_, err := m.client.Init(context.Background(), &proto.InitRequest{PluginConfig: pluginMap, DruidServer: brokerID, Cwd: cwd, ScrollConfig: config})
	if err != nil {
		return err
	}

	return nil
}

// Here is the gRPC server that GRPCClient talks to.
type GRPCServer struct {
	proto.PluginServer

	Impl   DruidPluginInterface
	broker *plugin.GRPCBroker
}

func (m *GRPCServer) GetModes(ctx context.Context, req *proto.EmptyPluginResponse) (*proto.GetModeResponse, error) {
	v, err := m.Impl.GetModes()
	return &proto.GetModeResponse{Modes: v}, err

}

func (m *GRPCServer) RunProcedure(ctx context.Context, req *proto.ProcedureRequest) (*proto.ProcedureResponse, error) {
	v, err := m.Impl.RunProcedure(req.Mode, req.Data)
	if err != nil {
		return nil, err
	}
	return &proto.ProcedureResponse{Data: v}, nil

}

func (m *GRPCServer) Init(ctx context.Context, req *proto.InitRequest) (*proto.EmptyPluginResponse, error) {

	conn, err := m.broker.Dial(req.DruidServer)
	if err != nil {
		return &proto.EmptyPluginResponse{}, err
	}

	a := &GRPCAddHelperClient{proto.NewDaemonServiceClient(conn)}

	err = m.Impl.Init(req.PluginConfig, a, req.Cwd, req.ScrollConfig)
	return &proto.EmptyPluginResponse{}, err

}

// GRPCClient is an implementation of KV that talks over RPC.
type GRPCAddHelperClient struct{ client proto.DaemonServiceClient }

func (m *GRPCAddHelperClient) NotifyConsole(mode string, data string) error {
	_, err := m.client.NotifyConsole(context.Background(), &proto.ConsoleNotification{
		Mode: mode,
		Data: data,
	})
	return err
}

// Here is the gRPC server that GRPCClient talks to.
type GRPCAddHelperServer struct {
	proto.DaemonServiceServer
	// This is the real implementation
	Impl DruidDaemon
}

func (m *GRPCAddHelperServer) NotifyConsole(ctx context.Context, req *proto.ConsoleNotification) (*proto.EmptyDaemonResponse, error) {
	err := m.Impl.NotifyConsole(req.Mode, req.Data)

	return &proto.EmptyDaemonResponse{}, err
}
