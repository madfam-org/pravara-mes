package sparkplug

import (
	"errors"
	"strings"
	"testing"
)

func TestStateJSON(t *testing.T) {
	b := EncodeState(HostState{Online: true, Timestamp: 1700000000000})
	if string(b) != `{"online":true,"timestamp":1700000000000}` {
		t.Fatalf("STATE json = %s", b)
	}
	s, err := DecodeState(b)
	if err != nil || !s.Online || s.Timestamp != 1700000000000 {
		t.Fatalf("decode = %+v %v", s, err)
	}
	for _, bad := range []string{`{"online":true}`, `{"timestamp":1}`, `ONLINE`, `{"online":"yes","timestamp":1}`} {
		if _, err := DecodeState([]byte(bad)); !errors.Is(err, ErrState) {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestHostTrackerIgnoresStaleState(t *testing.T) {
	var h HostTracker
	if h.Online() {
		t.Fatal("unknown host reported online")
	}
	if on, ch := h.Observe(HostState{Online: true, Timestamp: 100}); !on || !ch {
		t.Fatal("first online")
	}
	// An older offline (a will from a previous host session) must not take the host down.
	if on, ch := h.Observe(HostState{Online: false, Timestamp: 99}); !on || ch {
		t.Fatal("stale offline applied")
	}
	if on, ch := h.Observe(HostState{Online: false, Timestamp: 100}); on || !ch {
		t.Fatal("offline with equal timestamp must apply")
	}
	if on, ch := h.Observe(HostState{Online: false, Timestamp: 150}); on || ch {
		t.Fatal("repeat offline must not report a change")
	}
}

func TestEdgeNodeACLMatchesMES1(t *testing.T) {
	rules, err := EdgeNodeACL("acme", "site-north")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"publish   spBv1.0/acme/+/site-north",
		"publish   spBv1.0/acme/+/site-north/+",
		"subscribe spBv1.0/acme/NCMD/site-north",
		"subscribe spBv1.0/acme/DCMD/site-north/+",
		"subscribe spBv1.0/STATE/+",
	}, "\n") + "\n"
	if got := FormatACL(rules); got != want {
		t.Fatalf("ACL\n%s\nwant\n%s", got, want)
	}
	allow := []struct {
		a ACLAction
		t string
	}{
		{ACLPublish, "spBv1.0/acme/NBIRTH/site-north"},
		{ACLPublish, "spBv1.0/acme/NDEATH/site-north"},
		{ACLPublish, "spBv1.0/acme/DDATA/site-north/VORON-01"},
		{ACLSubscribe, "spBv1.0/acme/NCMD/site-north"},
		{ACLSubscribe, "spBv1.0/acme/DCMD/site-north/+"},
		{ACLSubscribe, "spBv1.0/acme/DCMD/site-north/VORON-01"},
		{ACLSubscribe, "spBv1.0/STATE/pravara-mes"},
		{ACLSubscribe, "spBv1.0/STATE/+"},
	}
	for _, c := range allow {
		if !Permits(rules, c.a, c.t) {
			t.Errorf("denied %s %s", c.a, c.t)
		}
	}
	deny := []struct {
		a ACLAction
		t string
	}{
		{ACLPublish, "spBv1.0/other/NBIRTH/site-north"},          // other tenant
		{ACLPublish, "spBv1.0/acme/NBIRTH/site-south"},           // other edge node
		{ACLPublish, "spBv1.0/acme/DDATA/site-south/VORON-01"},   // other edge's device
		{ACLPublish, "spBv1.0/STATE/pravara-mes"},                // impersonate the host
		{ACLPublish, "spBv1.0/acme/DDATA/site-north/VORON-01/x"}, // too deep
		{ACLPublish, "spBv1.0/acme/+/site-north"},                // wildcard publish
		{ACLSubscribe, "spBv1.0/acme/#"},                         // tenant-wide read
		{ACLSubscribe, "spBv1.0/acme/DDATA/site-north/+"},        // read own data back: not granted
		{ACLSubscribe, "spBv1.0/acme/DCMD/site-south/+"},         // other edge's commands
		{ACLSubscribe, "spBv1.0/acme/DCMD/+/+"},                  // all commands
		{ACLSubscribe, "spBv1.0/+/NCMD/site-north"},              // cross-tenant
		{ACLSubscribe, "#"},
		{ACLSubscribe, "$SYS/brokers"},
	}
	for _, c := range deny {
		if Permits(rules, c.a, c.t) {
			t.Errorf("allowed %s %s", c.a, c.t)
		}
	}
	if _, err := EdgeNodeACL("acme", "a/b"); err == nil {
		t.Fatal("invalid edge id accepted")
	}
}

func TestFilterCovers(t *testing.T) {
	for _, c := range []struct {
		f, r string
		want bool
	}{
		{"a/+/c", "a/b/c", true}, {"a/+/c", "a/+/c", true}, {"a/#", "a/b/c", true}, {"a/#", "a/#", true},
		{"a/+", "a/#", false}, {"a/+/c", "a/b", false}, {"a/b", "a/b/c", false}, {"#", "$SYS/x", false},
		{"+/x", "$SYS/x", false}, {"a/#", "a", true}, // MQTT 4.7.1.2: "a/#" also matches "a"
	} {
		if got := FilterCovers(c.f, c.r); got != c.want {
			t.Errorf("FilterCovers(%q, %q) = %v", c.f, c.r, got)
		}
	}
}
