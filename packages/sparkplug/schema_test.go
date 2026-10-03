package sparkplug

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/madfam-org/pravara-mes/packages/sparkplug/internal/schema"
	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

// specFields is the field table of the Sparkplug 3.0.0 payload definition
// ("Payload" chapter), transcribed as message.field=number:type:cardinality.
// It pins the generated code to the wire format: a change here or in the
// schema is a wire-compatibility break.
var specFields = []string{
	"Payload.timestamp=1:uint64:optional", "Payload.metrics=2:Metric:repeated",
	"Payload.seq=3:uint64:optional", "Payload.uuid=4:string:optional", "Payload.body=5:bytes:optional",
	"Payload.Template.version=1:string:optional", "Payload.Template.metrics=2:Metric:repeated",
	"Payload.Template.parameters=3:Parameter:repeated", "Payload.Template.template_ref=4:string:optional",
	"Payload.Template.is_definition=5:bool:optional",
	"Payload.Template.Parameter.name=1:string:optional", "Payload.Template.Parameter.type=2:uint32:optional",
	"Payload.Template.Parameter.int_value=3:uint32:oneof", "Payload.Template.Parameter.long_value=4:uint64:oneof",
	"Payload.Template.Parameter.float_value=5:float:oneof", "Payload.Template.Parameter.double_value=6:double:oneof",
	"Payload.Template.Parameter.boolean_value=7:bool:oneof", "Payload.Template.Parameter.string_value=8:string:oneof",
	"Payload.Template.Parameter.extension_value=9:ParameterValueExtension:oneof",
	"Payload.DataSet.num_of_columns=1:uint64:optional", "Payload.DataSet.columns=2:string:repeated",
	"Payload.DataSet.types=3:uint32:repeated", "Payload.DataSet.rows=4:Row:repeated",
	"Payload.DataSet.DataSetValue.int_value=1:uint32:oneof", "Payload.DataSet.DataSetValue.long_value=2:uint64:oneof",
	"Payload.DataSet.DataSetValue.float_value=3:float:oneof", "Payload.DataSet.DataSetValue.double_value=4:double:oneof",
	"Payload.DataSet.DataSetValue.boolean_value=5:bool:oneof", "Payload.DataSet.DataSetValue.string_value=6:string:oneof",
	"Payload.DataSet.DataSetValue.extension_value=7:DataSetValueExtension:oneof",
	"Payload.DataSet.Row.elements=1:DataSetValue:repeated",
	"Payload.PropertyValue.type=1:uint32:optional", "Payload.PropertyValue.is_null=2:bool:optional",
	"Payload.PropertyValue.int_value=3:uint32:oneof", "Payload.PropertyValue.long_value=4:uint64:oneof",
	"Payload.PropertyValue.float_value=5:float:oneof", "Payload.PropertyValue.double_value=6:double:oneof",
	"Payload.PropertyValue.boolean_value=7:bool:oneof", "Payload.PropertyValue.string_value=8:string:oneof",
	"Payload.PropertyValue.propertyset_value=9:PropertySet:oneof",
	"Payload.PropertyValue.propertysets_value=10:PropertySetList:oneof",
	"Payload.PropertyValue.extension_value=11:PropertyValueExtension:oneof",
	"Payload.PropertySet.keys=1:string:repeated", "Payload.PropertySet.values=2:PropertyValue:repeated",
	"Payload.PropertySetList.propertyset=1:PropertySet:repeated",
	"Payload.MetaData.is_multi_part=1:bool:optional", "Payload.MetaData.content_type=2:string:optional",
	"Payload.MetaData.size=3:uint64:optional", "Payload.MetaData.seq=4:uint64:optional",
	"Payload.MetaData.file_name=5:string:optional", "Payload.MetaData.file_type=6:string:optional",
	"Payload.MetaData.md5=7:string:optional", "Payload.MetaData.description=8:string:optional",
	"Payload.Metric.name=1:string:optional", "Payload.Metric.alias=2:uint64:optional",
	"Payload.Metric.timestamp=3:uint64:optional", "Payload.Metric.datatype=4:uint32:optional",
	"Payload.Metric.is_historical=5:bool:optional", "Payload.Metric.is_transient=6:bool:optional",
	"Payload.Metric.is_null=7:bool:optional", "Payload.Metric.metadata=8:MetaData:optional",
	"Payload.Metric.properties=9:PropertySet:optional",
	"Payload.Metric.int_value=10:uint32:oneof", "Payload.Metric.long_value=11:uint64:oneof",
	"Payload.Metric.float_value=12:float:oneof", "Payload.Metric.double_value=13:double:oneof",
	"Payload.Metric.boolean_value=14:bool:oneof", "Payload.Metric.string_value=15:string:oneof",
	"Payload.Metric.bytes_value=16:bytes:oneof", "Payload.Metric.dataset_value=17:DataSet:oneof",
	"Payload.Metric.template_value=18:Template:oneof",
	"Payload.Metric.extension_value=19:MetricValueExtension:oneof",
}

// specExtensionStarts is the first extension field number of each extensible message.
var specExtensionStarts = map[string]int{
	"Payload": 6, "Payload.Template": 6, "Payload.Template.Parameter.ParameterValueExtension": 1,
	"Payload.DataSet": 5, "Payload.DataSet.DataSetValue.DataSetValueExtension": 1, "Payload.DataSet.Row": 2,
	"Payload.PropertyValue.PropertyValueExtension": 1, "Payload.PropertySet": 3, "Payload.PropertySetList": 2,
	"Payload.MetaData": 9, "Payload.Metric.MetricValueExtension": 1,
}

func describe(md protoreflect.MessageDescriptor, prefix string, fields *[]string, ext map[string]int) {
	path := prefix + string(md.Name())
	for i := 0; i < md.Fields().Len(); i++ {
		f := md.Fields().Get(i)
		typ := f.Kind().String()
		if f.Message() != nil {
			typ = string(f.Message().Name())
		}
		card := "optional"
		switch {
		case f.IsList():
			card = "repeated"
		case f.ContainingOneof() != nil:
			card = "oneof"
		}
		*fields = append(*fields, fmt.Sprintf("%s.%s=%d:%s:%s", path, f.Name(), f.Number(), typ, card))
	}
	if md.ExtensionRanges().Len() > 0 {
		ext[path] = int(md.ExtensionRanges().Get(0)[0])
	}
	for i := 0; i < md.Messages().Len(); i++ {
		describe(md.Messages().Get(i), path+".", fields, ext)
	}
}

func TestGeneratedSchemaMatchesSpecification(t *testing.T) {
	var got []string
	ext := map[string]int{}
	describe((&pb.Payload{}).ProtoReflect().Descriptor(), "", &got, ext)
	want := append([]string(nil), specFields...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("schema drift\n got: %v\nwant: %v", got, want)
	}
	for msg, start := range specExtensionStarts {
		if ext[msg] != start {
			t.Errorf("%s: extensions start %d, want %d", msg, ext[msg], start)
		}
	}
	if len(ext) != len(specExtensionStarts) {
		t.Errorf("extensible messages = %d, want %d", len(ext), len(specExtensionStarts))
	}
	// DataType codes 0..34 (spec "Datatypes").
	ed := pb.DataType(0).Descriptor()
	if ed.Values().Len() != 35 {
		t.Fatalf("DataType has %d values, want 35", ed.Values().Len())
	}
	for code, name := range map[int32]string{0: "Unknown", 4: "Int64", 10: "Double", 11: "Boolean", 12: "String", 17: "Bytes", 31: "DoubleArray", 34: "DateTimeArray"} {
		if got := string(ed.Values().ByNumber(protoreflect.EnumNumber(code)).Name()); got != name {
			t.Errorf("DataType %d = %s, want %s", code, got, name)
		}
	}
}

func TestGeneratedCodeIsCurrent(t *testing.T) {
	got := protodesc.ToFileDescriptorProto(pb.File_sparkplug_b_proto)
	want := schema.FileDescriptor()
	want.Syntax = nil // protodesc omits the default "proto2"
	if !proto.Equal(got, want) {
		t.Fatal("sparkplugpb is stale: run `go generate ./sparkplugpb` in packages/sparkplug")
	}
}
