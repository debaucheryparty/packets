package v1

import (
	"context"
	"encoding/json"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	status "google.golang.org/grpc/status"
)

const JSONCodecName = "json"

type jsonCodec struct{}

func (jsonCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (jsonCodec) Name() string {
	return JSONCodecName
}

func init() {
	if encoding.GetCodec(JSONCodecName) == nil {
		encoding.RegisterCodec(jsonCodec{})
	}
}

type EnvCheckRequest struct {
	ProjectID   string       `json:"project_id"`
	ProjectRoot string       `json:"project_root"`
	Components  []*Component `json:"components"`
}

type Component struct {
	Type       string            `json:"type"`
	Name       string            `json:"name"`
	Path       string            `json:"path"`
	Confidence string            `json:"confidence"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type ToolchainRequirement struct {
	Name       string `json:"name"`
	Version    string `json:"version,omitempty"`
	Status     string `json:"status"`
	Details    string `json:"details,omitempty"`
	CanPrepare bool   `json:"can_prepare"`
	PrepareCmd string `json:"prepare_cmd,omitempty"`
}

type ComponentRequirements struct {
	Requirements []*ToolchainRequirement `json:"requirements"`
}

type EnvCheckResponse struct {
	ProjectRoot string                            `json:"project_root"`
	Components  map[string]*ComponentRequirements `json:"components"`
	AllReady    bool                              `json:"all_ready"`
}

type PrepareRequest struct {
	ProjectID   string       `json:"project_id"`
	ProjectRoot string       `json:"project_root"`
	Components  []*Component `json:"components"`
}

type PrepareProgressLine struct {
	Content string `json:"content"`
	Done    bool   `json:"done"`
	Error   bool   `json:"error"`
}

const (
	Environment_Check_FullMethodName   = "/packets.v1.Environment/Check"
	Environment_Prepare_FullMethodName = "/packets.v1.Environment/Prepare"
)

type EnvironmentClient interface {
	Check(ctx context.Context, in *EnvCheckRequest, opts ...grpc.CallOption) (*EnvCheckResponse, error)
	Prepare(ctx context.Context, in *PrepareRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[PrepareProgressLine], error)
}

type environmentClient struct {
	cc grpc.ClientConnInterface
}

func NewEnvironmentClient(cc grpc.ClientConnInterface) EnvironmentClient {
	return &environmentClient{cc}
}

func (c *environmentClient) Check(ctx context.Context, in *EnvCheckRequest, opts ...grpc.CallOption) (*EnvCheckResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod(), grpc.CallContentSubtype(JSONCodecName)}, opts...)
	out := new(EnvCheckResponse)
	err := c.cc.Invoke(ctx, Environment_Check_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *environmentClient) Prepare(ctx context.Context, in *PrepareRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[PrepareProgressLine], error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod(), grpc.CallContentSubtype(JSONCodecName)}, opts...)
	stream, err := c.cc.NewStream(ctx, &Environment_ServiceDesc.Streams[0], Environment_Prepare_FullMethodName, cOpts...)
	if err != nil {
		return nil, err
	}
	x := &grpc.GenericClientStream[PrepareRequest, PrepareProgressLine]{ClientStream: stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type EnvironmentServer interface {
	Check(context.Context, *EnvCheckRequest) (*EnvCheckResponse, error)
	Prepare(*PrepareRequest, grpc.ServerStreamingServer[PrepareProgressLine]) error
	mustEmbedUnimplementedEnvironmentServer()
}

type UnimplementedEnvironmentServer struct{}

func (UnimplementedEnvironmentServer) Check(context.Context, *EnvCheckRequest) (*EnvCheckResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method Check not implemented")
}

func (UnimplementedEnvironmentServer) Prepare(*PrepareRequest, grpc.ServerStreamingServer[PrepareProgressLine]) error {
	return status.Error(codes.Unimplemented, "method Prepare not implemented")
}
func (UnimplementedEnvironmentServer) mustEmbedUnimplementedEnvironmentServer() {}

func RegisterEnvironmentServer(s grpc.ServiceRegistrar, srv EnvironmentServer) {
	s.RegisterService(&Environment_ServiceDesc, srv)
}

func _Environment_Check_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(EnvCheckRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(EnvironmentServer).Check(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Environment_Check_FullMethodName,
	}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(EnvironmentServer).Check(ctx, req.(*EnvCheckRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Environment_Prepare_Handler(srv any, stream grpc.ServerStream) error {
	m := new(PrepareRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(EnvironmentServer).Prepare(m, &grpc.GenericServerStream[PrepareRequest, PrepareProgressLine]{ServerStream: stream})
}

var Environment_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "packets.v1.Environment",
	HandlerType: (*EnvironmentServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Check",
			Handler:    _Environment_Check_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Prepare",
			Handler:       _Environment_Prepare_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "proto/v1/environment.proto",
}
