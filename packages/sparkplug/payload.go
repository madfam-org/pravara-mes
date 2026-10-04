package sparkplug

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

// ErrValue is returned (wrapped) when a metric value does not fit its datatype.
var ErrValue = errors.New("sparkplug: invalid metric value")

// Encode serialises a payload to the Sparkplug B wire format.
func Encode(p *pb.Payload) ([]byte, error) {
	return proto.Marshal(p)
}

// Decode parses a Sparkplug B payload.
func Decode(b []byte) (*pb.Payload, error) {
	p := &pb.Payload{}
	if err := proto.Unmarshal(b, p); err != nil {
		return nil, fmt.Errorf("sparkplug: decode payload: %w", err)
	}
	return p, nil
}

// Millis converts a time to the Sparkplug timestamp form (ms since the Unix epoch, UTC).
func Millis(t time.Time) uint64 { return uint64(t.UnixMilli()) }

// NewMetric builds a named metric with the given datatype, value and timestamp.
// value must be a Go type compatible with dt (see SetValue). A nil value
// produces an is_null metric.
func NewMetric(name MetricName, dt pb.DataType, value any, ts time.Time) (*pb.Payload_Metric, error) {
	m := &pb.Payload_Metric{
		Name:      proto.String(string(name)),
		Timestamp: proto.Uint64(Millis(ts)),
		Datatype:  proto.Uint32(uint32(dt)),
	}
	if err := SetValue(m, dt, value); err != nil {
		return nil, fmt.Errorf("metric %q: %w", name, err)
	}
	return m, nil
}

// NewContractMetric builds a MES-1 metric using the contract datatype for its name.
func NewContractMetric(name MetricName, value any, ts time.Time) (*pb.Payload_Metric, error) {
	dt, ok := DatatypeOf(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q is not a MES-1 metric", ErrValue, name)
	}
	return NewMetric(name, dt, value, ts)
}

// SetValue stores value in the oneof field the specification assigns to dt.
func SetValue(m *pb.Payload_Metric, dt pb.DataType, value any) error {
	if value == nil {
		m.IsNull = proto.Bool(true)
		m.Value = nil
		return nil
	}
	m.IsNull = nil
	switch dt {
	case pb.DataType_Int8, pb.DataType_Int16, pb.DataType_Int32:
		v, err := asInt64(value)
		if err != nil {
			return err
		}
		if !fitsSigned(dt, v) {
			return fmt.Errorf("%w: %d overflows %s", ErrValue, v, dt)
		}
		m.Value = &pb.Payload_Metric_IntValue{IntValue: uint32(int32(v))}
	case pb.DataType_UInt8, pb.DataType_UInt16, pb.DataType_UInt32:
		v, err := asInt64(value)
		if err != nil {
			return err
		}
		if v < 0 || !fitsUnsigned(dt, uint64(v)) {
			return fmt.Errorf("%w: %d overflows %s", ErrValue, v, dt)
		}
		m.Value = &pb.Payload_Metric_IntValue{IntValue: uint32(v)}
	case pb.DataType_Int64:
		v, err := asInt64(value)
		if err != nil {
			return err
		}
		m.Value = &pb.Payload_Metric_LongValue{LongValue: uint64(v)}
	case pb.DataType_UInt64, pb.DataType_DateTime:
		switch v := value.(type) {
		case uint64:
			m.Value = &pb.Payload_Metric_LongValue{LongValue: v}
		case time.Time:
			m.Value = &pb.Payload_Metric_LongValue{LongValue: Millis(v)}
		default:
			i, err := asInt64(value)
			if err != nil || i < 0 {
				return fmt.Errorf("%w: %T for %s", ErrValue, value, dt)
			}
			m.Value = &pb.Payload_Metric_LongValue{LongValue: uint64(i)}
		}
	case pb.DataType_Float:
		f, err := asFloat64(value)
		if err != nil {
			return err
		}
		m.Value = &pb.Payload_Metric_FloatValue{FloatValue: float32(f)}
	case pb.DataType_Double:
		f, err := asFloat64(value)
		if err != nil {
			return err
		}
		m.Value = &pb.Payload_Metric_DoubleValue{DoubleValue: f}
	case pb.DataType_Boolean:
		b, ok := value.(bool)
		if !ok {
			return fmt.Errorf("%w: %T for Boolean", ErrValue, value)
		}
		m.Value = &pb.Payload_Metric_BooleanValue{BooleanValue: b}
	case pb.DataType_String, pb.DataType_Text, pb.DataType_UUID:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: %T for %s", ErrValue, value, dt)
		}
		m.Value = &pb.Payload_Metric_StringValue{StringValue: s}
	case pb.DataType_Bytes, pb.DataType_File:
		b, ok := value.([]byte)
		if !ok {
			return fmt.Errorf("%w: %T for %s", ErrValue, value, dt)
		}
		m.Value = &pb.Payload_Metric_BytesValue{BytesValue: b}
	case pb.DataType_DoubleArray:
		fs, ok := value.([]float64)
		if !ok {
			return fmt.Errorf("%w: %T for DoubleArray", ErrValue, value)
		}
		m.Value = &pb.Payload_Metric_BytesValue{BytesValue: EncodeDoubleArray(fs)}
	default:
		return fmt.Errorf("%w: datatype %s is not supported by this package", ErrValue, dt)
	}
	return nil
}

// Value extracts a metric value as a Go value. dt is the datatype learned from
// the birth certificate; pass DataType_Unknown to use the metric's own datatype
// field (present in births and in name-addressed commands).
//
// Returned types: int64 (signed ints), uint64 (unsigned ints, DateTime), float32,
// float64, bool, string, []byte, []float64 (DoubleArray) or nil for is_null.
func Value(m *pb.Payload_Metric, dt pb.DataType) (any, error) {
	if dt == pb.DataType_Unknown {
		dt = pb.DataType(m.GetDatatype())
	}
	if m.GetIsNull() {
		return nil, nil
	}
	switch dt {
	case pb.DataType_Int8:
		return int64(int8(m.GetIntValue())), nil
	case pb.DataType_Int16:
		return int64(int16(m.GetIntValue())), nil
	case pb.DataType_Int32:
		return int64(int32(m.GetIntValue())), nil
	case pb.DataType_UInt8, pb.DataType_UInt16, pb.DataType_UInt32:
		return uint64(m.GetIntValue()), nil
	case pb.DataType_Int64:
		return int64(m.GetLongValue()), nil
	case pb.DataType_UInt64, pb.DataType_DateTime:
		return m.GetLongValue(), nil
	case pb.DataType_Float:
		return m.GetFloatValue(), nil
	case pb.DataType_Double:
		return m.GetDoubleValue(), nil
	case pb.DataType_Boolean:
		return m.GetBooleanValue(), nil
	case pb.DataType_String, pb.DataType_Text, pb.DataType_UUID:
		return m.GetStringValue(), nil
	case pb.DataType_Bytes, pb.DataType_File:
		return m.GetBytesValue(), nil
	case pb.DataType_DoubleArray:
		return DecodeDoubleArray(m.GetBytesValue())
	default:
		return nil, fmt.Errorf("%w: datatype %s is not supported by this package", ErrValue, dt)
	}
}

// EncodeDoubleArray packs values as little-endian IEEE-754 doubles (spec 3.0, DoubleArray).
func EncodeDoubleArray(vs []float64) []byte {
	out := make([]byte, 8*len(vs))
	for i, v := range vs {
		binary.LittleEndian.PutUint64(out[8*i:], math.Float64bits(v))
	}
	return out
}

// DecodeDoubleArray is the inverse of EncodeDoubleArray.
func DecodeDoubleArray(b []byte) ([]float64, error) {
	if len(b)%8 != 0 {
		return nil, fmt.Errorf("%w: DoubleArray length %d is not a multiple of 8", ErrValue, len(b))
	}
	out := make([]float64, len(b)/8)
	for i := range out {
		out[i] = math.Float64frombits(binary.LittleEndian.Uint64(b[8*i:]))
	}
	return out, nil
}

func asInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int:
		return int64(x), nil
	case int8:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case int64:
		return x, nil
	case uint8:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint64:
		if x > math.MaxInt64 {
			return 0, fmt.Errorf("%w: %d overflows int64", ErrValue, x)
		}
		return int64(x), nil
	default:
		return 0, fmt.Errorf("%w: %T is not an integer", ErrValue, v)
	}
}

func asFloat64(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	default:
		i, err := asInt64(v)
		if err != nil {
			return 0, fmt.Errorf("%w: %T is not a number", ErrValue, v)
		}
		return float64(i), nil
	}
}

func fitsSigned(dt pb.DataType, v int64) bool {
	switch dt {
	case pb.DataType_Int8:
		return v >= math.MinInt8 && v <= math.MaxInt8
	case pb.DataType_Int16:
		return v >= math.MinInt16 && v <= math.MaxInt16
	default:
		return v >= math.MinInt32 && v <= math.MaxInt32
	}
}

func fitsUnsigned(dt pb.DataType, v uint64) bool {
	switch dt {
	case pb.DataType_UInt8:
		return v <= math.MaxUint8
	case pb.DataType_UInt16:
		return v <= math.MaxUint16
	default:
		return v <= math.MaxUint32
	}
}
