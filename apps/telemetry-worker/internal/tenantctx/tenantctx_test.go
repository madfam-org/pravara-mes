package tenantctx

import (
	"context"
	"testing"
)

func TestSegmentRoundTrip(t *testing.T) {
	if _, ok := TopicSegment(context.Background()); ok {
		t.Fatal("empty context must carry no segment")
	}
	ctx := WithTopicSegment(context.Background(), SegmentOf("acme/site/area/line/M1/ack"))
	got, ok := TopicSegment(ctx)
	if !ok || got != "acme" {
		t.Fatalf("got %q, %v", got, ok)
	}
	if _, ok := TopicSegment(WithTopicSegment(context.Background(), "")); ok {
		t.Fatal("blank segment must not count as bound")
	}
}
