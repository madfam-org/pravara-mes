package sparkplug

import (
	"fmt"
	"strings"
)

// ACLAction is the MQTT operation an ACL rule allows.
type ACLAction string

// ACL actions.
const (
	ACLPublish   ACLAction = "publish"
	ACLSubscribe ACLAction = "subscribe"
)

// ACLRule allows one action on a topic filter.
type ACLRule struct {
	Action ACLAction `json:"action"`
	Filter string    `json:"topic"`
}

// EdgeNodeACL returns the MES-1 §1 rules for one edge-node credential:
//
//	publish   spBv1.0/{group}/+/{edge}
//	publish   spBv1.0/{group}/+/{edge}/+
//	subscribe spBv1.0/{group}/NCMD/{edge}
//	subscribe spBv1.0/{group}/DCMD/{edge}/+
//	subscribe spBv1.0/STATE/+
//
// Everything else is denied.
func EdgeNodeACL(group, edge string) ([]ACLRule, error) {
	if err := validateGroup(group); err != nil {
		return nil, err
	}
	if err := ValidateID("edge_node_id", edge); err != nil {
		return nil, err
	}
	base := Namespace + "/" + group
	return []ACLRule{
		{ACLPublish, base + "/+/" + edge},
		{ACLPublish, base + "/+/" + edge + "/+"},
		{ACLSubscribe, base + "/" + string(NCMD) + "/" + edge},
		{ACLSubscribe, base + "/" + string(DCMD) + "/" + edge + "/+"},
		{ACLSubscribe, Namespace + "/" + string(STATE) + "/+"},
	}, nil
}

// Permits reports whether rules allow action on topic. For publish, topic
// must be a concrete topic; for subscribe it may be a filter, which is
// permitted only if every topic it can match is matched by one rule.
func Permits(rules []ACLRule, action ACLAction, topic string) bool {
	if action == ACLPublish && strings.ContainsAny(topic, "+#") {
		return false
	}
	for _, r := range rules {
		if r.Action == action && FilterCovers(r.Filter, topic) {
			return true
		}
	}
	return false
}

// FilterCovers reports whether MQTT filter covers every topic matched by
// requested (a concrete topic or another filter). Topics starting with '$' are
// never matched by a leading wildcard (MQTT 3.1.1 §4.7.2).
func FilterCovers(filter, requested string) bool {
	f := strings.Split(filter, "/")
	r := strings.Split(requested, "/")
	if len(r) > 0 && strings.HasPrefix(r[0], "$") && (f[0] == "+" || f[0] == "#") {
		return false
	}
	for i, fl := range f {
		if fl == "#" {
			return true
		}
		if i >= len(r) {
			return false
		}
		switch {
		case r[i] == "#":
			return false // requested multi-level wildcard is wider than any non-# level
		case fl == "+":
			continue
		case fl != r[i]:
			return false
		}
	}
	return len(f) == len(r)
}

// FormatACL renders rules one per line ("publish spBv1.0/..."), for operator review.
func FormatACL(rules []ACLRule) string {
	var b strings.Builder
	for _, r := range rules {
		fmt.Fprintf(&b, "%-9s %s\n", r.Action, r.Filter)
	}
	return b.String()
}
