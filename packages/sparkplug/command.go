package sparkplug

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

// ErrCommand is returned (wrapped) for a DCMD that does not meet MES-1 §1.
var ErrCommand = errors.New("sparkplug: invalid command")

// DeviceCommand is a MES-1 DCMD after decoding.
type DeviceCommand struct {
	ID                string
	Name              CommandName
	TaskID            string
	ArtifactURL       string
	ArtifactSHA256    string // lowercase hex
	ArtifactMediaType string
	Timestamp         uint64 // payload timestamp, ms
}

// ParseDeviceCommand decodes the MES-1 command metrics of a DCMD payload.
// Metrics may be addressed by name or by alias; resolve maps aliases (it may
// be nil when the host addresses metrics by name). Unknown metrics are an
// error so that a malformed command is never half-applied.
func ParseDeviceCommand(p *pb.Payload, resolve AliasResolver) (DeviceCommand, error) {
	if p.Seq != nil {
		return DeviceCommand{}, fmt.Errorf("%w: DCMD must not carry a seq", ErrCommand)
	}
	cmd := DeviceCommand{Timestamp: p.GetTimestamp()}
	for _, m := range p.GetMetrics() {
		name := MetricName(m.GetName())
		dt := pb.DataType(m.GetDatatype())
		if m.Name == nil {
			if m.Alias == nil || resolve == nil {
				return DeviceCommand{}, fmt.Errorf("%w: metric without name or resolvable alias", ErrCommand)
			}
			var ok bool
			var learned pb.DataType
			name, learned, ok = resolve(m.GetAlias())
			if !ok {
				return DeviceCommand{}, fmt.Errorf("%w: unknown alias %d", ErrCommand, m.GetAlias())
			}
			if m.Datatype == nil {
				dt = learned
			}
		}
		if dt == pb.DataType_Unknown {
			dt = pb.DataType_String
		}
		if dt != pb.DataType_String && dt != pb.DataType_Text {
			return DeviceCommand{}, fmt.Errorf("%w: %s must be a String metric", ErrCommand, name)
		}
		s := m.GetStringValue()
		switch name {
		case MetricCommandID:
			cmd.ID = s
		case MetricCommandName:
			cmd.Name = CommandName(s)
		case MetricCommandTaskID:
			cmd.TaskID = s
		case MetricCommandArtifactURL:
			cmd.ArtifactURL = s
		case MetricCommandArtifactSHA256:
			cmd.ArtifactSHA256 = strings.ToLower(s)
		case MetricCommandArtifactMediaType:
			cmd.ArtifactMediaType = s
		default:
			return DeviceCommand{}, fmt.Errorf("%w: unexpected metric %q", ErrCommand, name)
		}
	}
	return cmd, cmd.Validate()
}

// Validate applies the MES-1 rules: a command id, a known command name and,
// for start_job, an https artifact URL, a 64-hex sha256 and a media type.
func (c DeviceCommand) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("%w: Command/Id is required", ErrCommand)
	}
	if !c.Name.Valid() {
		return fmt.Errorf("%w: unknown Command/Name %q", ErrCommand, c.Name)
	}
	if c.Name != CommandStartJob {
		return nil
	}
	u, err := url.Parse(c.ArtifactURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("%w: Command/Artifact/Url must be an https URL", ErrCommand)
	}
	if len(c.ArtifactSHA256) != 64 {
		return fmt.Errorf("%w: Command/Artifact/Sha256 must be 64 hex characters", ErrCommand)
	}
	if _, err := hex.DecodeString(c.ArtifactSHA256); err != nil {
		return fmt.Errorf("%w: Command/Artifact/Sha256 is not hex", ErrCommand)
	}
	if c.ArtifactMediaType == "" {
		return fmt.Errorf("%w: Command/Artifact/MediaType is required", ErrCommand)
	}
	return nil
}

// BuildDeviceCommand encodes a DCMD payload with name-addressed metrics
// (payload timestamp, no seq). Hosts that prefer aliases may rewrite them.
func BuildDeviceCommand(c DeviceCommand, ts time.Time) (*pb.Payload, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	fields := []struct {
		name  MetricName
		value string
	}{
		{MetricCommandID, c.ID},
		{MetricCommandName, string(c.Name)},
		{MetricCommandTaskID, c.TaskID},
		{MetricCommandArtifactURL, c.ArtifactURL},
		{MetricCommandArtifactSHA256, c.ArtifactSHA256},
		{MetricCommandArtifactMediaType, c.ArtifactMediaType},
	}
	p := &pb.Payload{Timestamp: proto.Uint64(Millis(ts))}
	for _, f := range fields {
		if f.value == "" && f.name != MetricCommandID && f.name != MetricCommandName {
			continue
		}
		m, err := NewMetric(f.name, pb.DataType_String, f.value, ts)
		if err != nil {
			return nil, err
		}
		p.Metrics = append(p.Metrics, m)
	}
	return p, nil
}

// ParseRebirthRequest reports whether an NCMD payload asks for a rebirth
// ('Node Control/Rebirth' = true, by name or by alias).
func ParseRebirthRequest(p *pb.Payload, resolve AliasResolver) bool {
	for _, m := range p.GetMetrics() {
		name := MetricName(m.GetName())
		if m.Name == nil && m.Alias != nil && resolve != nil {
			name, _, _ = resolve(m.GetAlias())
		}
		if name == MetricNodeControlRebirth && m.GetBooleanValue() {
			return true
		}
	}
	return false
}
