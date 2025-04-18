// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.5
// 	protoc        (unknown)
// source: proto/cache/v1/service.proto

package v1

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
	reflect "reflect"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type GetRequest struct {
	state                 protoimpl.MessageState    `protogen:"opaque.v1"`
	xxx_hidden_ObjectPath string                    `protobuf:"bytes,1,opt,name=object_path,json=objectPath"`
	xxx_hidden_Keys       map[string]*emptypb.Empty `protobuf:"bytes,2,rep,name=keys" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"bytes,2,opt,name=value"`
	unknownFields         protoimpl.UnknownFields
	sizeCache             protoimpl.SizeCache
}

func (x *GetRequest) Reset() {
	*x = GetRequest{}
	mi := &file_proto_cache_v1_service_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetRequest) ProtoMessage() {}

func (x *GetRequest) ProtoReflect() protoreflect.Message {
	mi := &file_proto_cache_v1_service_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *GetRequest) GetObjectPath() string {
	if x != nil {
		return x.xxx_hidden_ObjectPath
	}
	return ""
}

func (x *GetRequest) GetKeys() map[string]*emptypb.Empty {
	if x != nil {
		return x.xxx_hidden_Keys
	}
	return nil
}

func (x *GetRequest) SetObjectPath(v string) {
	x.xxx_hidden_ObjectPath = v
}

func (x *GetRequest) SetKeys(v map[string]*emptypb.Empty) {
	x.xxx_hidden_Keys = v
}

type GetRequest_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	ObjectPath string
	Keys       map[string]*emptypb.Empty
}

func (b0 GetRequest_builder) Build() *GetRequest {
	m0 := &GetRequest{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_ObjectPath = b.ObjectPath
	x.xxx_hidden_Keys = b.Keys
	return m0
}

type GetResponse struct {
	state              protoimpl.MessageState    `protogen:"opaque.v1"`
	xxx_hidden_Found   map[string][]byte         `protobuf:"bytes,1,rep,name=found" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"bytes,2,opt,name=value"`
	xxx_hidden_Deleted map[string]*emptypb.Empty `protobuf:"bytes,2,rep,name=deleted" protobuf_key:"bytes,1,opt,name=key" protobuf_val:"bytes,2,opt,name=value"`
	unknownFields      protoimpl.UnknownFields
	sizeCache          protoimpl.SizeCache
}

func (x *GetResponse) Reset() {
	*x = GetResponse{}
	mi := &file_proto_cache_v1_service_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetResponse) ProtoMessage() {}

func (x *GetResponse) ProtoReflect() protoreflect.Message {
	mi := &file_proto_cache_v1_service_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *GetResponse) GetFound() map[string][]byte {
	if x != nil {
		return x.xxx_hidden_Found
	}
	return nil
}

func (x *GetResponse) GetDeleted() map[string]*emptypb.Empty {
	if x != nil {
		return x.xxx_hidden_Deleted
	}
	return nil
}

func (x *GetResponse) SetFound(v map[string][]byte) {
	x.xxx_hidden_Found = v
}

func (x *GetResponse) SetDeleted(v map[string]*emptypb.Empty) {
	x.xxx_hidden_Deleted = v
}

type GetResponse_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	Found   map[string][]byte
	Deleted map[string]*emptypb.Empty
}

func (b0 GetResponse_builder) Build() *GetResponse {
	m0 := &GetResponse{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_Found = b.Found
	x.xxx_hidden_Deleted = b.Deleted
	return m0
}

var File_proto_cache_v1_service_proto protoreflect.FileDescriptor

var file_proto_cache_v1_service_proto_rawDesc = string([]byte{
	0x0a, 0x1c, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x63, 0x61, 0x63, 0x68, 0x65, 0x2f, 0x76, 0x31,
	0x2f, 0x73, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x08,
	0x63, 0x61, 0x63, 0x68, 0x65, 0x2e, 0x76, 0x31, 0x1a, 0x1b, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65,
	0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2f, 0x65, 0x6d, 0x70, 0x74, 0x79, 0x2e,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0xb2, 0x01, 0x0a, 0x0a, 0x47, 0x65, 0x74, 0x52, 0x65, 0x71,
	0x75, 0x65, 0x73, 0x74, 0x12, 0x1f, 0x0a, 0x0b, 0x6f, 0x62, 0x6a, 0x65, 0x63, 0x74, 0x5f, 0x70,
	0x61, 0x74, 0x68, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0a, 0x6f, 0x62, 0x6a, 0x65, 0x63,
	0x74, 0x50, 0x61, 0x74, 0x68, 0x12, 0x32, 0x0a, 0x04, 0x6b, 0x65, 0x79, 0x73, 0x18, 0x02, 0x20,
	0x03, 0x28, 0x0b, 0x32, 0x1e, 0x2e, 0x63, 0x61, 0x63, 0x68, 0x65, 0x2e, 0x76, 0x31, 0x2e, 0x47,
	0x65, 0x74, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x2e, 0x4b, 0x65, 0x79, 0x73, 0x45, 0x6e,
	0x74, 0x72, 0x79, 0x52, 0x04, 0x6b, 0x65, 0x79, 0x73, 0x1a, 0x4f, 0x0a, 0x09, 0x4b, 0x65, 0x79,
	0x73, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x09, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x2c, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75,
	0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x16, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65,
	0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x45, 0x6d, 0x70, 0x74, 0x79, 0x52,
	0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x3a, 0x02, 0x38, 0x01, 0x22, 0x91, 0x02, 0x0a, 0x0b, 0x47,
	0x65, 0x74, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12, 0x36, 0x0a, 0x05, 0x66, 0x6f,
	0x75, 0x6e, 0x64, 0x18, 0x01, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x20, 0x2e, 0x63, 0x61, 0x63, 0x68,
	0x65, 0x2e, 0x76, 0x31, 0x2e, 0x47, 0x65, 0x74, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65,
	0x2e, 0x46, 0x6f, 0x75, 0x6e, 0x64, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x52, 0x05, 0x66, 0x6f, 0x75,
	0x6e, 0x64, 0x12, 0x3c, 0x0a, 0x07, 0x64, 0x65, 0x6c, 0x65, 0x74, 0x65, 0x64, 0x18, 0x02, 0x20,
	0x03, 0x28, 0x0b, 0x32, 0x22, 0x2e, 0x63, 0x61, 0x63, 0x68, 0x65, 0x2e, 0x76, 0x31, 0x2e, 0x47,
	0x65, 0x74, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x2e, 0x44, 0x65, 0x6c, 0x65, 0x74,
	0x65, 0x64, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x52, 0x07, 0x64, 0x65, 0x6c, 0x65, 0x74, 0x65, 0x64,
	0x1a, 0x38, 0x0a, 0x0a, 0x46, 0x6f, 0x75, 0x6e, 0x64, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10,
	0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x6b, 0x65, 0x79,
	0x12, 0x14, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0c, 0x52,
	0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x3a, 0x02, 0x38, 0x01, 0x1a, 0x52, 0x0a, 0x0c, 0x44, 0x65,
	0x6c, 0x65, 0x74, 0x65, 0x64, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65,
	0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x2c, 0x0a, 0x05,
	0x76, 0x61, 0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x16, 0x2e, 0x67, 0x6f,
	0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x45, 0x6d,
	0x70, 0x74, 0x79, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x3a, 0x02, 0x38, 0x01, 0x32, 0x44,
	0x0a, 0x0c, 0x43, 0x61, 0x63, 0x68, 0x65, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x12, 0x34,
	0x0a, 0x03, 0x47, 0x65, 0x74, 0x12, 0x14, 0x2e, 0x63, 0x61, 0x63, 0x68, 0x65, 0x2e, 0x76, 0x31,
	0x2e, 0x47, 0x65, 0x74, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x15, 0x2e, 0x63, 0x61,
	0x63, 0x68, 0x65, 0x2e, 0x76, 0x31, 0x2e, 0x47, 0x65, 0x74, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e,
	0x73, 0x65, 0x22, 0x00, 0x42, 0x35, 0x5a, 0x2e, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63,
	0x6f, 0x6d, 0x2f, 0x64, 0x79, 0x6e, 0x6f, 0x69, 0x6e, 0x63, 0x2f, 0x73, 0x6b, 0x79, 0x76, 0x61,
	0x75, 0x6c, 0x74, 0x2f, 0x67, 0x65, 0x6e, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x63, 0x61,
	0x63, 0x68, 0x65, 0x2f, 0x76, 0x31, 0x92, 0x03, 0x02, 0x08, 0x02, 0x62, 0x08, 0x65, 0x64, 0x69,
	0x74, 0x69, 0x6f, 0x6e, 0x73, 0x70, 0xe8, 0x07,
})

var file_proto_cache_v1_service_proto_msgTypes = make([]protoimpl.MessageInfo, 5)
var file_proto_cache_v1_service_proto_goTypes = []any{
	(*GetRequest)(nil),    // 0: cache.v1.GetRequest
	(*GetResponse)(nil),   // 1: cache.v1.GetResponse
	nil,                   // 2: cache.v1.GetRequest.KeysEntry
	nil,                   // 3: cache.v1.GetResponse.FoundEntry
	nil,                   // 4: cache.v1.GetResponse.DeletedEntry
	(*emptypb.Empty)(nil), // 5: google.protobuf.Empty
}
var file_proto_cache_v1_service_proto_depIdxs = []int32{
	2, // 0: cache.v1.GetRequest.keys:type_name -> cache.v1.GetRequest.KeysEntry
	3, // 1: cache.v1.GetResponse.found:type_name -> cache.v1.GetResponse.FoundEntry
	4, // 2: cache.v1.GetResponse.deleted:type_name -> cache.v1.GetResponse.DeletedEntry
	5, // 3: cache.v1.GetRequest.KeysEntry.value:type_name -> google.protobuf.Empty
	5, // 4: cache.v1.GetResponse.DeletedEntry.value:type_name -> google.protobuf.Empty
	0, // 5: cache.v1.CacheService.Get:input_type -> cache.v1.GetRequest
	1, // 6: cache.v1.CacheService.Get:output_type -> cache.v1.GetResponse
	6, // [6:7] is the sub-list for method output_type
	5, // [5:6] is the sub-list for method input_type
	5, // [5:5] is the sub-list for extension type_name
	5, // [5:5] is the sub-list for extension extendee
	0, // [0:5] is the sub-list for field type_name
}

func init() { file_proto_cache_v1_service_proto_init() }
func file_proto_cache_v1_service_proto_init() {
	if File_proto_cache_v1_service_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_proto_cache_v1_service_proto_rawDesc), len(file_proto_cache_v1_service_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   5,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_proto_cache_v1_service_proto_goTypes,
		DependencyIndexes: file_proto_cache_v1_service_proto_depIdxs,
		MessageInfos:      file_proto_cache_v1_service_proto_msgTypes,
	}.Build()
	File_proto_cache_v1_service_proto = out.File
	file_proto_cache_v1_service_proto_goTypes = nil
	file_proto_cache_v1_service_proto_depIdxs = nil
}
