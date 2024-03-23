// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.2.0
// - protoc             v3.21.8
// source: plugin/proto/plugin_service.proto

package proto

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// PluginClient is the client API for Plugin service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type PluginClient interface {
	GetModes(ctx context.Context, in *EmptyPluginResponse, opts ...grpc.CallOption) (*GetModeResponse, error)
	Init(ctx context.Context, in *InitRequest, opts ...grpc.CallOption) (*EmptyPluginResponse, error)
	RunProcedure(ctx context.Context, in *ProcedureRequest, opts ...grpc.CallOption) (*ProcedureResponse, error)
}

type pluginClient struct {
	cc grpc.ClientConnInterface
}

func NewPluginClient(cc grpc.ClientConnInterface) PluginClient {
	return &pluginClient{cc}
}

func (c *pluginClient) GetModes(ctx context.Context, in *EmptyPluginResponse, opts ...grpc.CallOption) (*GetModeResponse, error) {
	out := new(GetModeResponse)
	err := c.cc.Invoke(ctx, "/proto.Plugin/GetModes", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *pluginClient) Init(ctx context.Context, in *InitRequest, opts ...grpc.CallOption) (*EmptyPluginResponse, error) {
	out := new(EmptyPluginResponse)
	err := c.cc.Invoke(ctx, "/proto.Plugin/Init", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *pluginClient) RunProcedure(ctx context.Context, in *ProcedureRequest, opts ...grpc.CallOption) (*ProcedureResponse, error) {
	out := new(ProcedureResponse)
	err := c.cc.Invoke(ctx, "/proto.Plugin/RunProcedure", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PluginServer is the server API for Plugin service.
// All implementations must embed UnimplementedPluginServer
// for forward compatibility
type PluginServer interface {
	GetModes(context.Context, *EmptyPluginResponse) (*GetModeResponse, error)
	Init(context.Context, *InitRequest) (*EmptyPluginResponse, error)
	RunProcedure(context.Context, *ProcedureRequest) (*ProcedureResponse, error)
	mustEmbedUnimplementedPluginServer()
}

// UnimplementedPluginServer must be embedded to have forward compatible implementations.
type UnimplementedPluginServer struct {
}

func (UnimplementedPluginServer) GetModes(context.Context, *EmptyPluginResponse) (*GetModeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetModes not implemented")
}
func (UnimplementedPluginServer) Init(context.Context, *InitRequest) (*EmptyPluginResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Init not implemented")
}
func (UnimplementedPluginServer) RunProcedure(context.Context, *ProcedureRequest) (*ProcedureResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RunProcedure not implemented")
}
func (UnimplementedPluginServer) mustEmbedUnimplementedPluginServer() {}

// UnsafePluginServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to PluginServer will
// result in compilation errors.
type UnsafePluginServer interface {
	mustEmbedUnimplementedPluginServer()
}

func RegisterPluginServer(s grpc.ServiceRegistrar, srv PluginServer) {
	s.RegisterService(&Plugin_ServiceDesc, srv)
}

func _Plugin_GetModes_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(EmptyPluginResponse)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PluginServer).GetModes(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/proto.Plugin/GetModes",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PluginServer).GetModes(ctx, req.(*EmptyPluginResponse))
	}
	return interceptor(ctx, in, info, handler)
}

func _Plugin_Init_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(InitRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PluginServer).Init(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/proto.Plugin/Init",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PluginServer).Init(ctx, req.(*InitRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Plugin_RunProcedure_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ProcedureRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PluginServer).RunProcedure(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/proto.Plugin/RunProcedure",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PluginServer).RunProcedure(ctx, req.(*ProcedureRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// Plugin_ServiceDesc is the grpc.ServiceDesc for Plugin service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Plugin_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "proto.Plugin",
	HandlerType: (*PluginServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetModes",
			Handler:    _Plugin_GetModes_Handler,
		},
		{
			MethodName: "Init",
			Handler:    _Plugin_Init_Handler,
		},
		{
			MethodName: "RunProcedure",
			Handler:    _Plugin_RunProcedure_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "plugin/proto/plugin_service.proto",
}