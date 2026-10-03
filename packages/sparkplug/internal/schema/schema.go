// Package schema declares the Sparkplug B payload schema used by pravara.
package schema

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// This file is the single source of the Sparkplug B payload schema used by
// pravara. It was written from the payload chapter of the Eclipse Sparkplug
// 3.0.0 specification: every field number, wire type and cardinality below
// follows the specification's payload definition so that pravara interoperates
// with any conformant Sparkplug B implementation. Field names follow the names
// the specification uses in its prose, because the protobuf JSON mapping and
// tooling use them; they are not part of the binary wire format.
//
// The descriptor is declared in Go (not in a .proto file) so that the generated
// code can be rebuilt with `go generate` and no protoc installation. A
// human-readable rendering is written next to the generated code.

const (
	// ProtoFile is the descriptor file name of the schema.
	ProtoFile   = "sparkplug_b.proto"
	protoPkg    = "pravara.sparkplug.b"
	goPackage   = "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb;sparkplugpb"
	extMax      = 536870912 // protobuf "max" field number + 1 (exclusive end)
	payloadName = ".pravara.sparkplug.b.Payload"
)

type kind = descriptorpb.FieldDescriptorProto_Type

const (
	tUint32  = descriptorpb.FieldDescriptorProto_TYPE_UINT32
	tUint64  = descriptorpb.FieldDescriptorProto_TYPE_UINT64
	tFloat   = descriptorpb.FieldDescriptorProto_TYPE_FLOAT
	tDouble  = descriptorpb.FieldDescriptorProto_TYPE_DOUBLE
	tBool    = descriptorpb.FieldDescriptorProto_TYPE_BOOL
	tString  = descriptorpb.FieldDescriptorProto_TYPE_STRING
	tBytes   = descriptorpb.FieldDescriptorProto_TYPE_BYTES
	tMessage = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
)

// field builds an optional scalar field.
func field(name string, num int32, t kind) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		Number:   proto.Int32(num),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Type:     t.Enum(),
		JsonName: proto.String(jsonName(name)),
	}
}

// msgField builds an optional message-typed field.
func msgField(name string, num int32, typeName string) *descriptorpb.FieldDescriptorProto {
	f := field(name, num, tMessage)
	f.TypeName = proto.String(typeName)
	return f
}

// repeated marks a field as repeated.
func repeated(f *descriptorpb.FieldDescriptorProto) *descriptorpb.FieldDescriptorProto {
	f.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	return f
}

// inOneof places a field in the oneof with the given index.
func inOneof(idx int32, f *descriptorpb.FieldDescriptorProto) *descriptorpb.FieldDescriptorProto {
	f.OneofIndex = proto.Int32(idx)
	return f
}

// extensible returns an extension range "start to max".
func extensible(start int32) []*descriptorpb.DescriptorProto_ExtensionRange {
	return []*descriptorpb.DescriptorProto_ExtensionRange{{Start: proto.Int32(start), End: proto.Int32(extMax)}}
}

// valueExtension builds the empty, extensible message used by every "value"
// oneof as its extension slot.
func valueExtension(name string) *descriptorpb.DescriptorProto {
	return &descriptorpb.DescriptorProto{Name: proto.String(name), ExtensionRange: extensible(1)}
}

func oneofValue() []*descriptorpb.OneofDescriptorProto {
	return []*descriptorpb.OneofDescriptorProto{{Name: proto.String("value")}}
}

func jsonName(s string) string {
	out := make([]byte, 0, len(s))
	upper := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			upper = true
			continue
		}
		if upper && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		upper = false
		out = append(out, c)
	}
	return string(out)
}

// dataTypes lists the Sparkplug B datatype codes (spec 3.0, "Datatypes").
var dataTypes = []struct {
	name string
	code int32
}{
	{"Unknown", 0},
	{"Int8", 1}, {"Int16", 2}, {"Int32", 3}, {"Int64", 4},
	{"UInt8", 5}, {"UInt16", 6}, {"UInt32", 7}, {"UInt64", 8},
	{"Float", 9}, {"Double", 10}, {"Boolean", 11}, {"String", 12},
	{"DateTime", 13}, {"Text", 14},
	{"UUID", 15}, {"DataSet", 16}, {"Bytes", 17}, {"File", 18}, {"Template", 19},
	{"PropertySet", 20}, {"PropertySetList", 21},
	{"Int8Array", 22}, {"Int16Array", 23}, {"Int32Array", 24}, {"Int64Array", 25},
	{"UInt8Array", 26}, {"UInt16Array", 27}, {"UInt32Array", 28}, {"UInt64Array", 29},
	{"FloatArray", 30}, {"DoubleArray", 31}, {"BooleanArray", 32}, {"StringArray", 33},
	{"DateTimeArray", 34},
}

func p(n string) string { return payloadName + "." + n }

// FileDescriptor returns the complete schema.
func FileDescriptor() *descriptorpb.FileDescriptorProto {
	enumValues := make([]*descriptorpb.EnumValueDescriptorProto, 0, len(dataTypes))
	for _, d := range dataTypes {
		enumValues = append(enumValues, &descriptorpb.EnumValueDescriptorProto{
			Name: proto.String(d.name), Number: proto.Int32(d.code),
		})
	}

	parameter := &descriptorpb.DescriptorProto{
		Name: proto.String("Parameter"),
		Field: []*descriptorpb.FieldDescriptorProto{
			field("name", 1, tString),
			field("type", 2, tUint32),
			inOneof(0, field("int_value", 3, tUint32)),
			inOneof(0, field("long_value", 4, tUint64)),
			inOneof(0, field("float_value", 5, tFloat)),
			inOneof(0, field("double_value", 6, tDouble)),
			inOneof(0, field("boolean_value", 7, tBool)),
			inOneof(0, field("string_value", 8, tString)),
			inOneof(0, msgField("extension_value", 9, p("Template.Parameter.ParameterValueExtension"))),
		},
		NestedType: []*descriptorpb.DescriptorProto{valueExtension("ParameterValueExtension")},
		OneofDecl:  oneofValue(),
	}

	template := &descriptorpb.DescriptorProto{
		Name: proto.String("Template"),
		Field: []*descriptorpb.FieldDescriptorProto{
			field("version", 1, tString),
			repeated(msgField("metrics", 2, p("Metric"))),
			repeated(msgField("parameters", 3, p("Template.Parameter"))),
			field("template_ref", 4, tString),
			field("is_definition", 5, tBool),
		},
		NestedType:     []*descriptorpb.DescriptorProto{parameter},
		ExtensionRange: extensible(6),
	}

	dataSetValue := &descriptorpb.DescriptorProto{
		Name: proto.String("DataSetValue"),
		Field: []*descriptorpb.FieldDescriptorProto{
			inOneof(0, field("int_value", 1, tUint32)),
			inOneof(0, field("long_value", 2, tUint64)),
			inOneof(0, field("float_value", 3, tFloat)),
			inOneof(0, field("double_value", 4, tDouble)),
			inOneof(0, field("boolean_value", 5, tBool)),
			inOneof(0, field("string_value", 6, tString)),
			inOneof(0, msgField("extension_value", 7, p("DataSet.DataSetValue.DataSetValueExtension"))),
		},
		NestedType: []*descriptorpb.DescriptorProto{valueExtension("DataSetValueExtension")},
		OneofDecl:  oneofValue(),
	}
	row := &descriptorpb.DescriptorProto{
		Name:           proto.String("Row"),
		Field:          []*descriptorpb.FieldDescriptorProto{repeated(msgField("elements", 1, p("DataSet.DataSetValue")))},
		ExtensionRange: extensible(2),
	}
	dataSet := &descriptorpb.DescriptorProto{
		Name: proto.String("DataSet"),
		Field: []*descriptorpb.FieldDescriptorProto{
			field("num_of_columns", 1, tUint64),
			repeated(field("columns", 2, tString)),
			repeated(field("types", 3, tUint32)),
			repeated(msgField("rows", 4, p("DataSet.Row"))),
		},
		NestedType:     []*descriptorpb.DescriptorProto{dataSetValue, row},
		ExtensionRange: extensible(5),
	}

	propertyValue := &descriptorpb.DescriptorProto{
		Name: proto.String("PropertyValue"),
		Field: []*descriptorpb.FieldDescriptorProto{
			field("type", 1, tUint32),
			field("is_null", 2, tBool),
			inOneof(0, field("int_value", 3, tUint32)),
			inOneof(0, field("long_value", 4, tUint64)),
			inOneof(0, field("float_value", 5, tFloat)),
			inOneof(0, field("double_value", 6, tDouble)),
			inOneof(0, field("boolean_value", 7, tBool)),
			inOneof(0, field("string_value", 8, tString)),
			inOneof(0, msgField("propertyset_value", 9, p("PropertySet"))),
			inOneof(0, msgField("propertysets_value", 10, p("PropertySetList"))),
			inOneof(0, msgField("extension_value", 11, p("PropertyValue.PropertyValueExtension"))),
		},
		NestedType: []*descriptorpb.DescriptorProto{valueExtension("PropertyValueExtension")},
		OneofDecl:  oneofValue(),
	}
	propertySet := &descriptorpb.DescriptorProto{
		Name: proto.String("PropertySet"),
		Field: []*descriptorpb.FieldDescriptorProto{
			repeated(field("keys", 1, tString)),
			repeated(msgField("values", 2, p("PropertyValue"))),
		},
		ExtensionRange: extensible(3),
	}
	propertySetList := &descriptorpb.DescriptorProto{
		Name:           proto.String("PropertySetList"),
		Field:          []*descriptorpb.FieldDescriptorProto{repeated(msgField("propertyset", 1, p("PropertySet")))},
		ExtensionRange: extensible(2),
	}

	metaData := &descriptorpb.DescriptorProto{
		Name: proto.String("MetaData"),
		Field: []*descriptorpb.FieldDescriptorProto{
			field("is_multi_part", 1, tBool),
			field("content_type", 2, tString),
			field("size", 3, tUint64),
			field("seq", 4, tUint64),
			field("file_name", 5, tString),
			field("file_type", 6, tString),
			field("md5", 7, tString),
			field("description", 8, tString),
		},
		ExtensionRange: extensible(9),
	}

	metric := &descriptorpb.DescriptorProto{
		Name: proto.String("Metric"),
		Field: []*descriptorpb.FieldDescriptorProto{
			field("name", 1, tString),
			field("alias", 2, tUint64),
			field("timestamp", 3, tUint64),
			field("datatype", 4, tUint32),
			field("is_historical", 5, tBool),
			field("is_transient", 6, tBool),
			field("is_null", 7, tBool),
			msgField("metadata", 8, p("MetaData")),
			msgField("properties", 9, p("PropertySet")),
			inOneof(0, field("int_value", 10, tUint32)),
			inOneof(0, field("long_value", 11, tUint64)),
			inOneof(0, field("float_value", 12, tFloat)),
			inOneof(0, field("double_value", 13, tDouble)),
			inOneof(0, field("boolean_value", 14, tBool)),
			inOneof(0, field("string_value", 15, tString)),
			inOneof(0, field("bytes_value", 16, tBytes)),
			inOneof(0, msgField("dataset_value", 17, p("DataSet"))),
			inOneof(0, msgField("template_value", 18, p("Template"))),
			inOneof(0, msgField("extension_value", 19, p("Metric.MetricValueExtension"))),
		},
		NestedType: []*descriptorpb.DescriptorProto{valueExtension("MetricValueExtension")},
		OneofDecl:  oneofValue(),
	}

	payload := &descriptorpb.DescriptorProto{
		Name: proto.String("Payload"),
		Field: []*descriptorpb.FieldDescriptorProto{
			field("timestamp", 1, tUint64),
			repeated(msgField("metrics", 2, p("Metric"))),
			field("seq", 3, tUint64),
			field("uuid", 4, tString),
			field("body", 5, tBytes),
		},
		NestedType: []*descriptorpb.DescriptorProto{
			template, dataSet, propertyValue, propertySet, propertySetList, metaData, metric,
		},
		ExtensionRange: extensible(6),
	}

	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String(ProtoFile),
		Package: proto.String(protoPkg),
		Syntax:  proto.String("proto2"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String(goPackage)},
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("DataType"), Value: enumValues,
		}},
		MessageType: []*descriptorpb.DescriptorProto{payload},
	}
}
