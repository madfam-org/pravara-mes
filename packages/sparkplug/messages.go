package sparkplug

import (
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

// ErrMessage is returned (wrapped) when a built or received message breaks a
// Sparkplug payload rule.
var ErrMessage = errors.New("sparkplug: invalid message")

// NodeBirth builds an NBIRTH payload: payload timestamp, seq, the bdSeq metric
// (Int64, equal to the bdSeq of the CONNECT's will) and 'Node Control/Rebirth'
// = false, followed by any extra node metrics. Every metric must carry a name,
// alias, datatype and timestamp (see ValidateBirth).
func NodeBirth(bdSeq, seq uint64, ts time.Time, aliases *AliasTable, extra ...*pb.Payload_Metric) (*pb.Payload, error) {
	bd, err := NewMetric(MetricBdSeq, pb.DataType_Int64, int64(bdSeq), ts)
	if err != nil {
		return nil, err
	}
	rb, err := NewMetric(MetricNodeControlRebirth, pb.DataType_Boolean, false, ts)
	if err != nil {
		return nil, err
	}
	metrics := append([]*pb.Payload_Metric{bd, rb}, extra...)
	for _, m := range metrics {
		m.Alias = proto.Uint64(aliases.Assign("", MetricName(m.GetName()), pb.DataType(m.GetDatatype())))
	}
	p := &pb.Payload{Timestamp: proto.Uint64(Millis(ts)), Seq: proto.Uint64(seq), Metrics: metrics}
	return p, ValidateBirth(p)
}

// NodeDeath builds the NDEATH payload registered as the MQTT will: a payload
// timestamp and the bdSeq metric, and no seq (tck-id-payloads-ndeath-seq).
func NodeDeath(bdSeq uint64, ts time.Time) (*pb.Payload, error) {
	bd, err := NewMetric(MetricBdSeq, pb.DataType_Int64, int64(bdSeq), ts)
	if err != nil {
		return nil, err
	}
	return &pb.Payload{Timestamp: proto.Uint64(Millis(ts)), Metrics: []*pb.Payload_Metric{bd}}, nil
}

// DeviceBirth builds a DBIRTH payload, assigning each metric its node-unique alias.
func DeviceBirth(device string, seq uint64, ts time.Time, aliases *AliasTable, metrics []*pb.Payload_Metric) (*pb.Payload, error) {
	for _, m := range metrics {
		if m.Name == nil {
			return nil, fmt.Errorf("%w: DBIRTH metric without a name", ErrMessage)
		}
		m.Alias = proto.Uint64(aliases.Assign(device, MetricName(m.GetName()), pb.DataType(m.GetDatatype())))
	}
	p := &pb.Payload{Timestamp: proto.Uint64(Millis(ts)), Seq: proto.Uint64(seq), Metrics: metrics}
	return p, ValidateBirth(p)
}

// DeviceData builds a DDATA payload. Metrics are reduced to alias, timestamp
// and value (names and datatypes omitted, tck-id-payloads-alias-data-cmd-requirement);
// every metric must have been declared in the device's DBIRTH.
func DeviceData(device string, seq uint64, ts time.Time, aliases *AliasTable, metrics []*pb.Payload_Metric) (*pb.Payload, error) {
	out := make([]*pb.Payload_Metric, 0, len(metrics))
	for _, m := range metrics {
		alias, ok := aliases.Lookup(device, MetricName(m.GetName()))
		if !ok {
			return nil, fmt.Errorf("%w: metric %q was not declared in the DBIRTH of %q", ErrMessage, m.GetName(), device)
		}
		d := &pb.Payload_Metric{Alias: proto.Uint64(alias), Timestamp: m.Timestamp, IsNull: m.IsNull, Value: m.Value}
		if d.Timestamp == nil {
			d.Timestamp = proto.Uint64(Millis(ts))
		}
		out = append(out, d)
	}
	return &pb.Payload{Timestamp: proto.Uint64(Millis(ts)), Seq: proto.Uint64(seq), Metrics: out}, nil
}

// DeviceDeath builds a DDEATH payload: payload timestamp and seq, no metrics.
func DeviceDeath(seq uint64, ts time.Time) *pb.Payload {
	return &pb.Payload{Timestamp: proto.Uint64(Millis(ts)), Seq: proto.Uint64(seq)}
}

// ValidateBirth checks the NBIRTH/DBIRTH rules this package relies on:
// payload timestamp and seq present, seq ≤ 255, and every metric with a name,
// alias, datatype and timestamp.
func ValidateBirth(p *pb.Payload) error {
	if p.Timestamp == nil {
		return fmt.Errorf("%w: birth without a payload timestamp", ErrMessage)
	}
	if p.Seq == nil || p.GetSeq() > 255 {
		return fmt.Errorf("%w: birth seq must be 0..255", ErrMessage)
	}
	seen := map[uint64]string{}
	for _, m := range p.GetMetrics() {
		if m.Name == nil || m.Alias == nil || m.Datatype == nil || m.Timestamp == nil {
			return fmt.Errorf("%w: birth metric %q lacks name, alias, datatype or timestamp", ErrMessage, m.GetName())
		}
		if other, dup := seen[m.GetAlias()]; dup {
			return fmt.Errorf("%w: alias %d used by %q and %q", ErrMessage, m.GetAlias(), other, m.GetName())
		}
		seen[m.GetAlias()] = m.GetName()
	}
	return nil
}

// BdSeqOf returns the bdSeq metric of an NBIRTH or NDEATH payload.
func BdSeqOf(p *pb.Payload) (uint64, error) {
	for _, m := range p.GetMetrics() {
		if m.GetName() == string(MetricBdSeq) {
			if pb.DataType(m.GetDatatype()) != pb.DataType_Int64 {
				return 0, fmt.Errorf("%w: bdSeq must be Int64", ErrMessage)
			}
			return m.GetLongValue(), nil
		}
	}
	return 0, fmt.Errorf("%w: no bdSeq metric", ErrMessage)
}
