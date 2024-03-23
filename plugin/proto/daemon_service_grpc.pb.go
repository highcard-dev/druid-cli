// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.2.0
// - protoc             v3.21.8
// source: plugin/proto/daemon_service.proto

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

// DaemonServiceClient is the client API for DaemonService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type DaemonServiceClient interface {
	NotifyConsole(ctx context.Context, in *ConsoleNotification, opts ...grpc.CallOption) (*EmptyDaemonResponse, error)
}

type daemonServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewDaemonServiceClient(cc grpc.ClientConnInterface) DaemonServiceClient {
	return &daemonServiceClient{cc}
}

func (c *daemonServiceClient) NotifyConsole(ctx context.Context, in *ConsoleNotification, opts ...grpc.CallOption) (*EmptyDaemonResponse, error) {
	out := new(EmptyDaemonResponse)
	err := c.cc.Invoke(ctx, "/proto.DaemonService/NotifyConsole", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DaemonServiceServer is the server API for DaemonService service.
// All implementations must embed UnimplementedDaemonServiceServer
// for forward compatibility
type DaemonServiceServer interface {
	NotifyConsole(context.Context, *ConsoleNotification) (*EmptyDaemonResponse, error)
	mustEmbedUnimplementedDaemonServiceServer()
}

// UnimplementedDaemonServiceServer must be embedded to have forward compatible implementations.
type UnimplementedDaemonServiceServer struct {
}

func (UnimplementedDaemonServiceServer) NotifyConsole(context.Context, *ConsoleNotification) (*EmptyDaemonResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method NotifyConsole not implemented")
}
func (UnimplementedDaemonServiceServer) mustEmbedUnimplementedDaemonServiceServer() {}

// UnsafeDaemonServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to DaemonServiceServer will
// result in compilation errors.
type UnsafeDaemonServiceServer interface {
	mustEmbedUnimplementedDaemonServiceServer()
}

func RegisterDaemonServiceServer(s grpc.ServiceRegistrar, srv DaemonServiceServer) {
	s.RegisterService(&DaemonService_ServiceDesc, srv)
}

func _DaemonService_NotifyConsole_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ConsoleNotification)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DaemonServiceServer).NotifyConsole(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/proto.DaemonService/NotifyConsole",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DaemonServiceServer).NotifyConsole(ctx, req.(*ConsoleNotification))
	}
	return interceptor(ctx, in, info, handler)
}

// DaemonService_ServiceDesc is the grpc.ServiceDesc for DaemonService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var DaemonService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "proto.DaemonService",
	HandlerType: (*DaemonServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "NotifyConsole",
			Handler:    _DaemonService_NotifyConsole_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "plugin/proto/daemon_service.proto",
}