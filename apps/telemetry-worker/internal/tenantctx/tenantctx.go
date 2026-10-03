// Package tenantctx carries the tenant segment of an MQTT topic through a
// context, so the store can scope every statement to that tenant.
//
// Until per-node credentials exist, the tenant of an MQTT message is the
// first segment of its topic ({tenant}/{site}/{area}/{line}/{machine}/...),
// given either as the tenant's UUID or as its slug.
package tenantctx

import (
	"context"
	"strings"
)

type key struct{}

// WithTopicSegment returns ctx carrying the topic's tenant segment.
func WithTopicSegment(ctx context.Context, segment string) context.Context {
	return context.WithValue(ctx, key{}, strings.TrimSpace(segment))
}

// TopicSegment returns the tenant segment bound to ctx, if any.
func TopicSegment(ctx context.Context) (string, bool) {
	s, ok := ctx.Value(key{}).(string)
	return s, ok && s != ""
}

// SegmentOf returns the tenant segment (first level) of an MQTT topic.
func SegmentOf(topic string) string {
	return strings.SplitN(topic, "/", 2)[0]
}
