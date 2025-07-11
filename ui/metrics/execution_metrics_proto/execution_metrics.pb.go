// Copyright 2024 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.30.0
// 	protoc        v3.21.12
// source: execution_metrics.proto

package execution_metrics_proto

import (
	find_input_delta_proto "android/soong/cmd/find_input_delta/find_input_delta_proto"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// These field numbers are also found in the inner message declarations.
// We verify that the values are the same, and that every enum value is checked
// in execution_metrics_test.go.
// Do not change this enum without also updating:
//   - the submessage's .proto file
//   - execution_metrics_test.go
type FieldNumbers int32

const (
	FieldNumbers_FIELD_NUMBERS_UNSPECIFIED FieldNumbers = 0
	FieldNumbers_FIELD_NUMBERS_FILE_LIST   FieldNumbers = 1
)

// Enum value maps for FieldNumbers.
var (
	FieldNumbers_name = map[int32]string{
		0: "FIELD_NUMBERS_UNSPECIFIED",
		1: "FIELD_NUMBERS_FILE_LIST",
	}
	FieldNumbers_value = map[string]int32{
		"FIELD_NUMBERS_UNSPECIFIED": 0,
		"FIELD_NUMBERS_FILE_LIST":   1,
	}
)

func (x FieldNumbers) Enum() *FieldNumbers {
	p := new(FieldNumbers)
	*p = x
	return p
}

func (x FieldNumbers) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (FieldNumbers) Descriptor() protoreflect.EnumDescriptor {
	return file_execution_metrics_proto_enumTypes[0].Descriptor()
}

func (FieldNumbers) Type() protoreflect.EnumType {
	return &file_execution_metrics_proto_enumTypes[0]
}

func (x FieldNumbers) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Do not use.
func (x *FieldNumbers) UnmarshalJSON(b []byte) error {
	num, err := protoimpl.X.UnmarshalJSONEnum(x.Descriptor(), b)
	if err != nil {
		return err
	}
	*x = FieldNumbers(num)
	return nil
}

// Deprecated: Use FieldNumbers.Descriptor instead.
func (FieldNumbers) EnumDescriptor() ([]byte, []int) {
	return file_execution_metrics_proto_rawDescGZIP(), []int{0}
}

type SoongExecutionMetrics struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// cmd/find_input_delta/find_input_delta_proto.FileList
	FileList *find_input_delta_proto.FileList `protobuf:"bytes,1,opt,name=file_list,json=fileList" json:"file_list,omitempty"`
}

func (x *SoongExecutionMetrics) Reset() {
	*x = SoongExecutionMetrics{}
	if protoimpl.UnsafeEnabled {
		mi := &file_execution_metrics_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SoongExecutionMetrics) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SoongExecutionMetrics) ProtoMessage() {}

func (x *SoongExecutionMetrics) ProtoReflect() protoreflect.Message {
	mi := &file_execution_metrics_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SoongExecutionMetrics.ProtoReflect.Descriptor instead.
func (*SoongExecutionMetrics) Descriptor() ([]byte, []int) {
	return file_execution_metrics_proto_rawDescGZIP(), []int{0}
}

func (x *SoongExecutionMetrics) GetFileList() *find_input_delta_proto.FileList {
	if x != nil {
		return x.FileList
	}
	return nil
}

var File_execution_metrics_proto protoreflect.FileDescriptor

var file_execution_metrics_proto_rawDesc = []byte{
	0x0a, 0x17, 0x65, 0x78, 0x65, 0x63, 0x75, 0x74, 0x69, 0x6f, 0x6e, 0x5f, 0x6d, 0x65, 0x74, 0x72,
	0x69, 0x63, 0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x13, 0x73, 0x6f, 0x6f, 0x6e, 0x67,
	0x5f, 0x62, 0x75, 0x69, 0x6c, 0x64, 0x5f, 0x6d, 0x65, 0x74, 0x72, 0x69, 0x63, 0x73, 0x1a, 0x3b,
	0x63, 0x6d, 0x64, 0x2f, 0x66, 0x69, 0x6e, 0x64, 0x5f, 0x69, 0x6e, 0x70, 0x75, 0x74, 0x5f, 0x64,
	0x65, 0x6c, 0x74, 0x61, 0x2f, 0x66, 0x69, 0x6e, 0x64, 0x5f, 0x69, 0x6e, 0x70, 0x75, 0x74, 0x5f,
	0x64, 0x65, 0x6c, 0x74, 0x61, 0x5f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x66, 0x69, 0x6c, 0x65,
	0x5f, 0x6c, 0x69, 0x73, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0x5e, 0x0a, 0x15, 0x53,
	0x6f, 0x6f, 0x6e, 0x67, 0x45, 0x78, 0x65, 0x63, 0x75, 0x74, 0x69, 0x6f, 0x6e, 0x4d, 0x65, 0x74,
	0x72, 0x69, 0x63, 0x73, 0x12, 0x45, 0x0a, 0x09, 0x66, 0x69, 0x6c, 0x65, 0x5f, 0x6c, 0x69, 0x73,
	0x74, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x28, 0x2e, 0x61, 0x6e, 0x64, 0x72, 0x6f, 0x69,
	0x64, 0x2e, 0x66, 0x69, 0x6e, 0x64, 0x5f, 0x69, 0x6e, 0x70, 0x75, 0x74, 0x5f, 0x64, 0x65, 0x6c,
	0x74, 0x61, 0x5f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2e, 0x46, 0x69, 0x6c, 0x65, 0x4c, 0x69, 0x73,
	0x74, 0x52, 0x08, 0x66, 0x69, 0x6c, 0x65, 0x4c, 0x69, 0x73, 0x74, 0x2a, 0x4a, 0x0a, 0x0c, 0x46,
	0x69, 0x65, 0x6c, 0x64, 0x4e, 0x75, 0x6d, 0x62, 0x65, 0x72, 0x73, 0x12, 0x1d, 0x0a, 0x19, 0x46,
	0x49, 0x45, 0x4c, 0x44, 0x5f, 0x4e, 0x55, 0x4d, 0x42, 0x45, 0x52, 0x53, 0x5f, 0x55, 0x4e, 0x53,
	0x50, 0x45, 0x43, 0x49, 0x46, 0x49, 0x45, 0x44, 0x10, 0x00, 0x12, 0x1b, 0x0a, 0x17, 0x46, 0x49,
	0x45, 0x4c, 0x44, 0x5f, 0x4e, 0x55, 0x4d, 0x42, 0x45, 0x52, 0x53, 0x5f, 0x46, 0x49, 0x4c, 0x45,
	0x5f, 0x4c, 0x49, 0x53, 0x54, 0x10, 0x01, 0x42, 0x32, 0x5a, 0x30, 0x61, 0x6e, 0x64, 0x72, 0x6f,
	0x69, 0x64, 0x2f, 0x73, 0x6f, 0x6f, 0x6e, 0x67, 0x2f, 0x75, 0x69, 0x2f, 0x6d, 0x65, 0x74, 0x72,
	0x69, 0x63, 0x73, 0x2f, 0x65, 0x78, 0x65, 0x63, 0x75, 0x74, 0x69, 0x6f, 0x6e, 0x5f, 0x6d, 0x65,
	0x74, 0x72, 0x69, 0x63, 0x73, 0x5f, 0x70, 0x72, 0x6f, 0x74, 0x6f,
}

var (
	file_execution_metrics_proto_rawDescOnce sync.Once
	file_execution_metrics_proto_rawDescData = file_execution_metrics_proto_rawDesc
)

func file_execution_metrics_proto_rawDescGZIP() []byte {
	file_execution_metrics_proto_rawDescOnce.Do(func() {
		file_execution_metrics_proto_rawDescData = protoimpl.X.CompressGZIP(file_execution_metrics_proto_rawDescData)
	})
	return file_execution_metrics_proto_rawDescData
}

var file_execution_metrics_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_execution_metrics_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_execution_metrics_proto_goTypes = []interface{}{
	(FieldNumbers)(0),                       // 0: soong_build_metrics.FieldNumbers
	(*SoongExecutionMetrics)(nil),           // 1: soong_build_metrics.SoongExecutionMetrics
	(*find_input_delta_proto.FileList)(nil), // 2: android.find_input_delta_proto.FileList
}
var file_execution_metrics_proto_depIdxs = []int32{
	2, // 0: soong_build_metrics.SoongExecutionMetrics.file_list:type_name -> android.find_input_delta_proto.FileList
	1, // [1:1] is the sub-list for method output_type
	1, // [1:1] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_execution_metrics_proto_init() }
func file_execution_metrics_proto_init() {
	if File_execution_metrics_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_execution_metrics_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SoongExecutionMetrics); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_execution_metrics_proto_rawDesc,
			NumEnums:      1,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_execution_metrics_proto_goTypes,
		DependencyIndexes: file_execution_metrics_proto_depIdxs,
		EnumInfos:         file_execution_metrics_proto_enumTypes,
		MessageInfos:      file_execution_metrics_proto_msgTypes,
	}.Build()
	File_execution_metrics_proto = out.File
	file_execution_metrics_proto_rawDesc = nil
	file_execution_metrics_proto_goTypes = nil
	file_execution_metrics_proto_depIdxs = nil
}
