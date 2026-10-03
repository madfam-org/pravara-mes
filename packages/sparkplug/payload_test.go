package sparkplug

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

var t0 = time.UnixMilli(1000).UTC() // varint E8 07

// Golden vectors are written out byte by byte from the protobuf encoding rules,
// independently of the Go protobuf runtime.
var goldenVectors = []struct {
	name    string
	hex     string
	payload *pb.Payload
}{
	{
		// NDEATH: timestamp=1000, metrics=[{name:"bdSeq", timestamp:1000, datatype:Int64(4), long_value:3}]
		name: "ndeath_bdseq_3",
		hex: "08e807" + "120e" + "0a05" + hex.EncodeToString([]byte("bdSeq")) +
			"18e807" + "2004" + "5803",
		payload: &pb.Payload{Timestamp: proto.Uint64(1000), Metrics: []*pb.Payload_Metric{{
			Name: proto.String("bdSeq"), Timestamp: proto.Uint64(1000), Datatype: proto.Uint32(4),
			Value: &pb.Payload_Metric_LongValue{LongValue: 3},
		}}},
	},
	{
		// DDATA: timestamp=1000, metrics=[{alias:5, timestamp:1000, double_value:21.5}], seq=7
		name: "ddata_alias_double",
		hex:  "08e807" + "120e" + "1005" + "18e807" + "69" + "0000000000803540" + "1807",
		payload: &pb.Payload{Timestamp: proto.Uint64(1000), Seq: proto.Uint64(7), Metrics: []*pb.Payload_Metric{{
			Alias: proto.Uint64(5), Timestamp: proto.Uint64(1000),
			Value: &pb.Payload_Metric_DoubleValue{DoubleValue: 21.5},
		}}},
	},
	{
		// DBIRTH metric: name "State/Status", alias 9, timestamp 1000, datatype String(12), string "idle"; seq 0
		name: "dbirth_string",
		hex: "08e807" + "121b" + "0a0c" + hex.EncodeToString([]byte("State/Status")) + "1009" + "18e807" + "200c" +
			"7a04" + hex.EncodeToString([]byte("idle")) + "1800",
		payload: &pb.Payload{Timestamp: proto.Uint64(1000), Seq: proto.Uint64(0), Metrics: []*pb.Payload_Metric{{
			Name: proto.String("State/Status"), Alias: proto.Uint64(9), Timestamp: proto.Uint64(1000),
			Datatype: proto.Uint32(12), Value: &pb.Payload_Metric_StringValue{StringValue: "idle"},
		}}},
	},
}

func TestGoldenVectors(t *testing.T) {
	for _, g := range goldenVectors {
		t.Run(g.name, func(t *testing.T) {
			want, err := hex.DecodeString(g.hex)
			if err != nil {
				t.Fatal(err)
			}
			got, err := proto.MarshalOptions{Deterministic: true}.Marshal(g.payload)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("encode\n got %x\nwant %x", got, want)
			}
			dec, err := Decode(want)
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(dec, g.payload) {
				t.Fatalf("decode mismatch: %v", dec)
			}
		})
	}
}

func TestDecodeKeepsUnknownExtensionFields(t *testing.T) {
	// Payload with timestamp=1000 plus an extension field 6 (varint 1) a third party may send.
	b, _ := hex.DecodeString("08e8073001")
	p, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if p.GetTimestamp() != 1000 {
		t.Fatalf("timestamp = %d", p.GetTimestamp())
	}
	again, _ := Encode(p)
	if !bytes.Equal(again, b) {
		t.Fatalf("re-encode lost the extension: %x", again)
	}
	if _, err := Decode([]byte{0x0a, 0xff}); err == nil {
		t.Fatal("truncated payload decoded without error")
	}
}

func TestValueRoundTrip(t *testing.T) {
	cases := []struct {
		dt   pb.DataType
		in   any
		want any
	}{
		{pb.DataType_Int8, -5, int64(-5)},
		{pb.DataType_Int16, int16(-300), int64(-300)},
		{pb.DataType_Int32, int32(-1), int64(-1)},
		{pb.DataType_Int64, int64(math.MinInt64), int64(math.MinInt64)},
		{pb.DataType_UInt8, 200, uint64(200)},
		{pb.DataType_UInt32, uint32(math.MaxUint32), uint64(math.MaxUint32)},
		{pb.DataType_UInt64, uint64(math.MaxUint64), uint64(math.MaxUint64)},
		{pb.DataType_DateTime, t0, uint64(1000)},
		{pb.DataType_Float, float32(1.5), float32(1.5)},
		{pb.DataType_Double, 210.25, 210.25},
		{pb.DataType_Boolean, true, true},
		{pb.DataType_String, "printing", "printing"},
		{pb.DataType_Bytes, []byte{1, 2}, []byte{1, 2}},
	}
	for _, c := range cases {
		m, err := NewMetric("x", c.dt, c.in, t0)
		if err != nil {
			t.Fatalf("%s: %v", c.dt, err)
		}
		b, _ := Encode(&pb.Payload{Metrics: []*pb.Payload_Metric{m}})
		p, err := Decode(b)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Value(p.Metrics[0], pb.DataType_Unknown)
		if err != nil {
			t.Fatalf("%s: %v", c.dt, err)
		}
		if gb, ok := got.([]byte); ok {
			if !bytes.Equal(gb, c.want.([]byte)) {
				t.Errorf("%s: got %v", c.dt, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %#v want %#v", c.dt, got, c.want)
		}
	}
	arr, err := NewMetric("n", pb.DataType_DoubleArray, []float64{0.4, 0.6}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(arr.GetBytesValue()) != "9a9999999999d93f333333333333e33f" {
		t.Fatalf("DoubleArray bytes = %x", arr.GetBytesValue())
	}
	v, err := Value(arr, pb.DataType_Unknown)
	if err != nil || len(v.([]float64)) != 2 || v.([]float64)[1] != 0.6 {
		t.Fatalf("DoubleArray decode = %v %v", v, err)
	}
	null, _ := NewMetric("z", pb.DataType_String, nil, t0)
	if v, _ := Value(null, pb.DataType_Unknown); v != nil || !null.GetIsNull() {
		t.Fatal("nil value must produce is_null")
	}
}

func TestValueRejectsMismatches(t *testing.T) {
	for _, c := range []struct {
		dt pb.DataType
		v  any
	}{
		{pb.DataType_Int8, 128}, {pb.DataType_UInt8, -1}, {pb.DataType_UInt16, 70000},
		{pb.DataType_Boolean, "true"}, {pb.DataType_String, 3}, {pb.DataType_Template, "x"},
	} {
		if _, err := NewMetric("x", c.dt, c.v, t0); !errors.Is(err, ErrValue) {
			t.Errorf("%s with %#v: err = %v", c.dt, c.v, err)
		}
	}
	if _, err := DecodeDoubleArray([]byte{1, 2, 3}); !errors.Is(err, ErrValue) {
		t.Error("short DoubleArray accepted")
	}
	if _, err := NewContractMetric("Not/AContract", "x", t0); !errors.Is(err, ErrValue) {
		t.Error("non-contract metric accepted")
	}
}
