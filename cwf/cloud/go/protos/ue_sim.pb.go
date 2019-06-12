// Code generated by protoc-gen-go. DO NOT EDIT.
// source: cwf/protos/ue_sim.proto

package protos

import (
	context "context"
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	grpc "google.golang.org/grpc"
	protos "magma/orc8r/cloud/go/protos"
	math "math"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion3 // please upgrade the proto package

// --------------------------------------------------------------------------
// UE config
// --------------------------------------------------------------------------
type UEConfig struct {
	// Unique identifier for the UE.
	Imsi string `protobuf:"bytes,1,opt,name=imsi,proto3" json:"imsi,omitempty"`
	// Authentication key (k).
	AuthKey []byte `protobuf:"bytes,2,opt,name=auth_key,json=authKey,proto3" json:"auth_key,omitempty"`
	// Operator configuration field (Op) signed with authentication key (k).
	AuthOpc              []byte   `protobuf:"bytes,3,opt,name=auth_opc,json=authOpc,proto3" json:"auth_opc,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *UEConfig) Reset()         { *m = UEConfig{} }
func (m *UEConfig) String() string { return proto.CompactTextString(m) }
func (*UEConfig) ProtoMessage()    {}
func (*UEConfig) Descriptor() ([]byte, []int) {
	return fileDescriptor_01bc05ea16f96cbc, []int{0}
}

func (m *UEConfig) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_UEConfig.Unmarshal(m, b)
}
func (m *UEConfig) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_UEConfig.Marshal(b, m, deterministic)
}
func (m *UEConfig) XXX_Merge(src proto.Message) {
	xxx_messageInfo_UEConfig.Merge(m, src)
}
func (m *UEConfig) XXX_Size() int {
	return xxx_messageInfo_UEConfig.Size(m)
}
func (m *UEConfig) XXX_DiscardUnknown() {
	xxx_messageInfo_UEConfig.DiscardUnknown(m)
}

var xxx_messageInfo_UEConfig proto.InternalMessageInfo

func (m *UEConfig) GetImsi() string {
	if m != nil {
		return m.Imsi
	}
	return ""
}

func (m *UEConfig) GetAuthKey() []byte {
	if m != nil {
		return m.AuthKey
	}
	return nil
}

func (m *UEConfig) GetAuthOpc() []byte {
	if m != nil {
		return m.AuthOpc
	}
	return nil
}

func init() {
	proto.RegisterType((*UEConfig)(nil), "magma.cwf.UEConfig")
}

func init() { proto.RegisterFile("cwf/protos/ue_sim.proto", fileDescriptor_01bc05ea16f96cbc) }

var fileDescriptor_01bc05ea16f96cbc = []byte{
	// 203 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0x12, 0x4f, 0x2e, 0x4f, 0xd3,
	0x2f, 0x28, 0xca, 0x2f, 0xc9, 0x2f, 0xd6, 0x2f, 0x4d, 0x8d, 0x2f, 0xce, 0xcc, 0xd5, 0x03, 0xf3,
	0x84, 0x38, 0x73, 0x13, 0xd3, 0x73, 0x13, 0xf5, 0x92, 0xcb, 0xd3, 0xa4, 0x24, 0xf3, 0x8b, 0x92,
	0x2d, 0x8a, 0x60, 0xaa, 0x92, 0xf3, 0x73, 0x73, 0xf3, 0xf3, 0x20, 0xaa, 0x94, 0x42, 0xb8, 0x38,
	0x42, 0x5d, 0x9d, 0xf3, 0xf3, 0xd2, 0x32, 0xd3, 0x85, 0x84, 0xb8, 0x58, 0x32, 0x73, 0x8b, 0x33,
	0x25, 0x18, 0x15, 0x18, 0x35, 0x38, 0x83, 0xc0, 0x6c, 0x21, 0x49, 0x2e, 0x8e, 0xc4, 0xd2, 0x92,
	0x8c, 0xf8, 0xec, 0xd4, 0x4a, 0x09, 0x26, 0x05, 0x46, 0x0d, 0x9e, 0x20, 0x76, 0x10, 0xdf, 0x3b,
	0xb5, 0x12, 0x2e, 0x95, 0x5f, 0x90, 0x2c, 0xc1, 0x8c, 0x90, 0xf2, 0x2f, 0x48, 0x36, 0xb2, 0xe2,
	0x62, 0x0d, 0x75, 0x0d, 0xce, 0xcc, 0x15, 0x32, 0xe4, 0x62, 0x75, 0x4c, 0x49, 0x09, 0x75, 0x15,
	0x12, 0xd6, 0x83, 0x3b, 0x47, 0x0f, 0x66, 0xa1, 0x94, 0x20, 0x54, 0x10, 0xec, 0x3c, 0xbd, 0xb0,
	0xfc, 0xcc, 0x14, 0x25, 0x06, 0x27, 0xe9, 0x28, 0x49, 0xb0, 0xa8, 0x3e, 0xc8, 0x63, 0xc9, 0x39,
	0xf9, 0xa5, 0x29, 0xfa, 0xe9, 0xf9, 0x50, 0xb7, 0x27, 0xb1, 0x81, 0x69, 0x63, 0x40, 0x00, 0x00,
	0x00, 0xff, 0xff, 0x78, 0x13, 0x09, 0x20, 0xf6, 0x00, 0x00, 0x00,
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// UESimClient is the client API for UESim service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type UESimClient interface {
	// Adds a new UE to the store.
	// Throws ALREADY_EXISTS if the UE already exists.
	//
	AddUE(ctx context.Context, in *UEConfig, opts ...grpc.CallOption) (*protos.Void, error)
}

type uESimClient struct {
	cc *grpc.ClientConn
}

func NewUESimClient(cc *grpc.ClientConn) UESimClient {
	return &uESimClient{cc}
}

func (c *uESimClient) AddUE(ctx context.Context, in *UEConfig, opts ...grpc.CallOption) (*protos.Void, error) {
	out := new(protos.Void)
	err := c.cc.Invoke(ctx, "/magma.cwf.UESim/AddUE", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UESimServer is the server API for UESim service.
type UESimServer interface {
	// Adds a new UE to the store.
	// Throws ALREADY_EXISTS if the UE already exists.
	//
	AddUE(context.Context, *UEConfig) (*protos.Void, error)
}

func RegisterUESimServer(s *grpc.Server, srv UESimServer) {
	s.RegisterService(&_UESim_serviceDesc, srv)
}

func _UESim_AddUE_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UEConfig)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(UESimServer).AddUE(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/magma.cwf.UESim/AddUE",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(UESimServer).AddUE(ctx, req.(*UEConfig))
	}
	return interceptor(ctx, in, info, handler)
}

var _UESim_serviceDesc = grpc.ServiceDesc{
	ServiceName: "magma.cwf.UESim",
	HandlerType: (*UESimServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "AddUE",
			Handler:    _UESim_AddUE_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "cwf/protos/ue_sim.proto",
}
