package edge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

func TestMaterialClassMapping(t *testing.T) {
	cases := map[string]string{
		// Bambu AMS tray_type
		"PLA": "pla", "PETG": "petg", "ABS": "abs", "ASA": "asa", "PC": "pc", "TPU": "tpu-95a",
		"PA-CF": "pa-cf", "PAHT-CF": "pa12-cf", "ABS-GF": "abs-gf", "PETG-HF": "petg",
		"TPU-AMS": "", "PLA-CF": "", "PVA": "", "SUPPORT": "",
		// Moonraker filament_type / Spoolman material
		" pla ": "pla", "Pla+": "pla", "PETG  HF": "petg", "TPU 85A": "tpu-85a", "TPU 95A": "tpu-95a",
		"": "", "unknown": "",
	}
	for in, want := range cases {
		if got := MaterialClass(in); got != want {
			t.Errorf("MaterialClass(%q) = %q, want %q", in, got, want)
		}
	}
	// Every mapped class is a material-classes key for an FFF process.
	fff := map[string]bool{"pla": true, "petg": true, "abs": true, "abs-gf": true, "asa": true, "pc": true,
		"tpu-95a": true, "tpu-85a": true, "pa12-cf": true, "pa-cf": true}
	for vendor, class := range materialClasses {
		if !fff[class] {
			t.Errorf("%q maps to %q, which is not an FFF material-classes key", vendor, class)
		}
	}
}

func TestDeriveCapabilities(t *testing.T) {
	reg := registry.NewRegistry()
	def, _ := reg.GetDefinition("bambu_a1")
	caps, err := deriveCapabilities(def, "bambu_mqtt", map[string]interface{}{
		"nozzle_diameters_mm": []interface{}{0.4, 0.6}, "enclosure": false, "toolhead_count": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[sparkplug.CapabilityKey]any{
		sparkplug.CapabilityProcess: "fff", sparkplug.CapabilityBuildVolumeXMM: 256.0, sparkplug.CapabilityMaxHotendTempC: 300.0,
		sparkplug.CapabilityMaxBedTempC: 100.0, sparkplug.CapabilityMaterialSlots: int64(4), sparkplug.CapabilityFirmware: "bambu",
		sparkplug.CapabilityConnectivity: "bambu_lan_mqtt", sparkplug.CapabilityEnclosure: false, sparkplug.CapabilityToolheadCount: int64(1),
	}
	for k, v := range want {
		if caps[k] != v {
			t.Errorf("%s = %#v, want %#v", k, caps[k], v)
		}
	}
	if d, ok := caps[sparkplug.CapabilityNozzleDiametersMM].([]float64); !ok || len(d) != 2 || d[1] != 0.6 {
		t.Errorf("nozzle diameters = %#v", caps[sparkplug.CapabilityNozzleDiametersMM])
	}
	if _, err := deriveCapabilities(def, "bambu_mqtt", map[string]interface{}{"enclosure": "yes"}); err == nil {
		t.Error("string accepted for a boolean capability")
	}
	if _, err := deriveCapabilities(nil, "moonraker", map[string]interface{}{"max_hotend_temp_c": "hot"}); err == nil {
		t.Error("string accepted for a numeric capability")
	}
}

func TestArtifactFetcher(t *testing.T) {
	body := []byte("G28\nG1 X1\n")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write(body)
		case "/big":
			_, _ = w.Write([]byte(strings.Repeat("x", 100)))
		case "/redirect":
			http.Redirect(w, r, "http://example.invalid/a", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	f := NewArtifactFetcher(dir, 64, 5*time.Second)
	f.Client.Transport = srv.Client().Transport
	ctx := context.Background()

	path, n, err := f.Fetch(ctx, srv.URL+"/ok", digest(body))
	if err != nil || n != int64(len(body)) {
		t.Fatalf("fetch = %v %d", err, n)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(body) {
		t.Fatal("content")
	}
	_ = os.Remove(path)

	path, _, err = f.Fetch(ctx, srv.URL+"/ok", strings.ToUpper(digest(body)))
	if err != nil {
		t.Fatalf("upper-case digest: %v", err)
	}
	_ = os.Remove(path)
	if _, _, err := f.Fetch(ctx, srv.URL+"/ok", digest([]byte("other"))); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("mismatch err = %v", err)
	}
	if _, _, err := f.Fetch(ctx, srv.URL+"/big", digest(body)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("size limit err = %v", err)
	}
	if _, _, err := f.Fetch(ctx, srv.URL+"/redirect", digest(body)); err == nil {
		t.Fatal("redirect to http followed")
	}
	if _, _, err := f.Fetch(ctx, strings.Replace(srv.URL, "https", "http", 1)+"/ok", digest(body)); err == nil {
		t.Fatal("http URL accepted")
	}
	if _, _, err := f.Fetch(ctx, srv.URL+"/missing", digest(body)); err == nil {
		t.Fatal("404 accepted")
	}
	if _, _, err := f.Fetch(ctx, srv.URL+"/ok", "abc"); err == nil {
		t.Fatal("short digest accepted")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		t.Errorf("left behind: %s", e.Name())
	}
}

func TestConfigValidation(t *testing.T) {
	base := func() Config {
		c := Config{GroupID: "acme", EdgeNodeID: "site-north", BrokerURL: "ssl://127.0.0.1:8883",
			Devices: []DeviceConfig{{DeviceID: "VORON-01", Definition: "voron_2_4", Host: "printer.local"}}}
		c.ApplyDefaults()
		return c
	}
	if err := (func() *Config { c := base(); return &c })().Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	bad := []func(*Config){
		func(c *Config) { c.GroupID = "a/b" },
		func(c *Config) { c.GroupID = "STATE" },
		func(c *Config) { c.EdgeNodeID = "" },
		func(c *Config) { c.BrokerURL = "tcp://broker.example.test:1883" },
		func(c *Config) { c.BrokerURL = "tcp://127.0.0.1:1883" }, // loopback still needs the explicit opt-in
		func(c *Config) { c.BrokerURL = "ws://127.0.0.1:8083" },
		func(c *Config) { c.TLSClientCertFile = "cert.pem" },
		func(c *Config) { c.Devices = nil },
		func(c *Config) { c.Devices = append(c.Devices, c.Devices[0]) },
		func(c *Config) { c.Devices[0].Host = "" },
		func(c *Config) { c.Devices[0].Definition = "" },
		func(c *Config) { c.Devices[0].DeviceID = "a+b" },
	}
	for i, f := range bad {
		c := base()
		c.Devices = append([]DeviceConfig(nil), c.Devices...)
		f(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	c := base()
	c.BrokerURL, c.AllowPlaintextBroker = "tcp://127.0.0.1:1883", true
	if err := c.Validate(); err != nil {
		t.Errorf("loopback plaintext with opt-in rejected: %v", err)
	}
	if base().PrimaryHostID != sparkplug.PrimaryHostID || !c.WaitsForPrimaryHost() {
		t.Error("primary host defaults to pravara-mes")
	}
}

func TestArtifactFileName(t *testing.T) {
	sha := digest([]byte("x"))
	if got := artifactFileName("task/../42 x", sha, "text/x-gcode"); got != "pravara-task_.._42_x-"+sha[:12]+".gcode" {
		t.Errorf("got %q", got)
	}
	if got := artifactFileName("t", sha, "model/3mf"); !strings.HasSuffix(got, ".3mf") || strings.ContainsAny(got, "/\\") {
		t.Errorf("got %q", got)
	}
	if !fileMatches("/sdcard/pravara-t-x.3mf", "pravara-t-x.3mf") || fileMatches("", "a") || fileMatches("b", "a") {
		t.Error("fileMatches")
	}
}
