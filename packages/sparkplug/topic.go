package sparkplug

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Namespace is the Sparkplug B topic namespace element (spec 3.0, "Topic Namespace Elements").
const Namespace = "spBv1.0"

// MessageType is the Sparkplug message type topic element.
type MessageType string

// Sparkplug message types.
const (
	NBIRTH MessageType = "NBIRTH"
	NDEATH MessageType = "NDEATH"
	NDATA  MessageType = "NDATA"
	NCMD   MessageType = "NCMD"
	DBIRTH MessageType = "DBIRTH"
	DDEATH MessageType = "DDEATH"
	DDATA  MessageType = "DDATA"
	DCMD   MessageType = "DCMD"
	STATE  MessageType = "STATE"
)

// IsNodeLevel reports whether the type addresses an edge node (no device id).
func (t MessageType) IsNodeLevel() bool {
	switch t {
	case NBIRTH, NDEATH, NDATA, NCMD:
		return true
	}
	return false
}

// IsDeviceLevel reports whether the type addresses a device under an edge node.
func (t MessageType) IsDeviceLevel() bool {
	switch t {
	case DBIRTH, DDEATH, DDATA, DCMD:
		return true
	}
	return false
}

// PrimaryHostID is the Sparkplug host application id of pravara's telemetry
// worker (MES-1 §1): its STATE topic is spBv1.0/STATE/pravara-mes.
const PrimaryHostID = "pravara-mes"

// SiteEdgeNodeID returns the MES-1 edge node id for a site box: "site-<slug>".
func SiteEdgeNodeID(siteSlug string) string { return "site-" + siteSlug }

// ErrInvalidTopic is returned (wrapped) for any topic or id that violates the namespace rules.
var ErrInvalidTopic = errors.New("sparkplug: invalid topic")

// ValidateID checks a group, edge node, device or host id: non-empty, valid
// UTF-8 and free of the MQTT reserved characters '+', '/' and '#'.
func ValidateID(kind, id string) error {
	if id == "" {
		return fmt.Errorf("%w: empty %s", ErrInvalidTopic, kind)
	}
	if !utf8.ValidString(id) {
		return fmt.Errorf("%w: %s is not valid UTF-8", ErrInvalidTopic, kind)
	}
	if strings.ContainsAny(id, "+/#") {
		return fmt.Errorf("%w: %s %q contains a reserved character", ErrInvalidTopic, kind, id)
	}
	return nil
}

func validateGroup(group string) error {
	if err := ValidateID("group_id", group); err != nil {
		return err
	}
	if group == string(STATE) {
		return fmt.Errorf("%w: group_id %q collides with the STATE topic", ErrInvalidTopic, group)
	}
	return nil
}

// Topic is a parsed Sparkplug topic.
type Topic struct {
	GroupID    string
	Type       MessageType
	EdgeNodeID string
	DeviceID   string // device-level types only
	HostID     string // STATE only
}

// String renders the topic. It does not validate; use the constructors for that.
func (t Topic) String() string {
	switch {
	case t.Type == STATE:
		return Namespace + "/" + string(STATE) + "/" + t.HostID
	case t.Type.IsDeviceLevel():
		return strings.Join([]string{Namespace, t.GroupID, string(t.Type), t.EdgeNodeID, t.DeviceID}, "/")
	default:
		return strings.Join([]string{Namespace, t.GroupID, string(t.Type), t.EdgeNodeID}, "/")
	}
}

// NodeTopic builds spBv1.0/{group}/{NBIRTH|NDEATH|NDATA|NCMD}/{edge}.
func NodeTopic(group string, mt MessageType, edge string) (string, error) {
	if !mt.IsNodeLevel() {
		return "", fmt.Errorf("%w: %s is not a node-level message type", ErrInvalidTopic, mt)
	}
	if err := validateGroup(group); err != nil {
		return "", err
	}
	if err := ValidateID("edge_node_id", edge); err != nil {
		return "", err
	}
	return Topic{GroupID: group, Type: mt, EdgeNodeID: edge}.String(), nil
}

// DeviceTopic builds spBv1.0/{group}/{DBIRTH|DDEATH|DDATA|DCMD}/{edge}/{device}.
func DeviceTopic(group string, mt MessageType, edge, device string) (string, error) {
	if !mt.IsDeviceLevel() {
		return "", fmt.Errorf("%w: %s is not a device-level message type", ErrInvalidTopic, mt)
	}
	if err := validateGroup(group); err != nil {
		return "", err
	}
	if err := ValidateID("edge_node_id", edge); err != nil {
		return "", err
	}
	if err := ValidateID("device_id", device); err != nil {
		return "", err
	}
	return Topic{GroupID: group, Type: mt, EdgeNodeID: edge, DeviceID: device}.String(), nil
}

// StateTopic builds spBv1.0/STATE/{host_id}.
func StateTopic(hostID string) (string, error) {
	if err := ValidateID("host_id", hostID); err != nil {
		return "", err
	}
	return Topic{Type: STATE, HostID: hostID}.String(), nil
}

// ParseTopic parses and validates a concrete (wildcard-free) Sparkplug topic.
func ParseTopic(s string) (Topic, error) {
	parts := strings.Split(s, "/")
	if len(parts) < 3 || parts[0] != Namespace {
		return Topic{}, fmt.Errorf("%w: %q is not in the %s namespace", ErrInvalidTopic, s, Namespace)
	}
	if parts[1] == string(STATE) {
		if len(parts) != 3 {
			return Topic{}, fmt.Errorf("%w: STATE topic must have 3 levels: %q", ErrInvalidTopic, s)
		}
		if err := ValidateID("host_id", parts[2]); err != nil {
			return Topic{}, err
		}
		return Topic{Type: STATE, HostID: parts[2]}, nil
	}
	if len(parts) < 4 {
		return Topic{}, fmt.Errorf("%w: %q is too short", ErrInvalidTopic, s)
	}
	t := Topic{GroupID: parts[1], Type: MessageType(parts[2]), EdgeNodeID: parts[3]}
	if err := validateGroup(t.GroupID); err != nil {
		return Topic{}, err
	}
	if err := ValidateID("edge_node_id", t.EdgeNodeID); err != nil {
		return Topic{}, err
	}
	switch {
	case t.Type.IsNodeLevel():
		if len(parts) != 4 {
			return Topic{}, fmt.Errorf("%w: node topic must have 4 levels: %q", ErrInvalidTopic, s)
		}
	case t.Type.IsDeviceLevel():
		if len(parts) != 5 {
			return Topic{}, fmt.Errorf("%w: device topic must have 5 levels: %q", ErrInvalidTopic, s)
		}
		t.DeviceID = parts[4]
		if err := ValidateID("device_id", t.DeviceID); err != nil {
			return Topic{}, err
		}
	default:
		return Topic{}, fmt.Errorf("%w: unknown message type %q", ErrInvalidTopic, parts[2])
	}
	return t, nil
}
