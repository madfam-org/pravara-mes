package simulator

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// DefaultMotionCadence is the simulator's notification interval. Klipper
// refreshes subscriptions every 0.25 s (klippy/webhooks.py
// SUBSCRIPTION_REFRESH_TIME = .25) and recomputes motion_report every 0.25 s
// (klippy/extras/motion_report.py STATUS_REFRESH_TIME = 0.250), both at
// Klipper3d/klipper@461c4e3.
const DefaultMotionCadence = 250 * time.Millisecond

// MotionEmission is one live_position the simulator sent in a
// notify_status_update (or a subscribe result).
type MotionEmission struct {
	At       time.Time
	Position [4]float64
	Velocity float64
	Homed    string
}

// Moonraker simulates the Moonraker HTTP/WebSocket API of a Klipper printer.
type Moonraker struct {
	APIKey string

	srv *httptest.Server

	mu            sync.Mutex
	down          bool
	klippy        string
	state         string // standby | printing | paused | complete | cancelled | error
	file          string
	progress      float64 // 0..1
	extruder, bed float64
	spoolMaterial string // "" = no active Spoolman spool
	fileFilament  string // filament_type in file metadata
	files         map[string][]byte
	calls         []string

	// Motion (gcode.go): a path played from pathStart, notifications every cadence.
	cadence   time.Duration
	path      *GCodePath
	pathStart time.Time
	rest      [4]float64 // position when no path plays
	homed     string
	emissions []MotionEmission
	clockZero time.Time
}

// NewMoonraker returns a simulator that requires apiKey when it is non-empty.
func NewMoonraker(apiKey string) *Moonraker {
	return &Moonraker{APIKey: apiKey, klippy: "ready", state: "standby", extruder: 24, bed: 23, files: map[string][]byte{},
		cadence: DefaultMotionCadence, clockZero: time.Now()}
}

// Start listens on a loopback port.
func (m *Moonraker) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/server/info", m.handleInfo)
	mux.HandleFunc("/printer/objects/query", m.handleQuery)
	mux.HandleFunc("/server/files/upload", m.handleUpload)
	mux.HandleFunc("/printer/print/start", m.handleStart)
	mux.HandleFunc("/printer/print/pause", m.transition("pause", "printing", "paused"))
	mux.HandleFunc("/printer/print/resume", m.transition("resume", "paused", "printing"))
	mux.HandleFunc("/printer/print/cancel", m.transition("cancel", "", "cancelled"))
	mux.HandleFunc("/printer/emergency_stop", m.transition("emergency_stop", "", "error"))
	mux.HandleFunc("/printer/gcode/script", m.handleScript)
	mux.HandleFunc("/server/spoolman/spool_id", m.handleSpoolID)
	mux.HandleFunc("/server/spoolman/proxy", m.handleSpoolProxy)
	mux.HandleFunc("/server/files/metadata", m.handleMetadata)
	mux.HandleFunc("/websocket", m.handleWebsocket)
	m.srv = httptest.NewUnstartedServer(m.gate(mux))
	m.srv.Start()
}

// Close stops the simulator.
func (m *Moonraker) Close() { m.srv.Close() }

// HostPort returns the listening address.
func (m *Moonraker) HostPort() (string, int) {
	host, port, _ := net.SplitHostPort(m.srv.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	return host, p
}

// SetDown makes every request fail with 503 (printer unreachable) or recovers it.
func (m *Moonraker) SetDown(down bool) { m.with(func() { m.down = down }) }

// SetTemps sets the reported extruder and bed temperatures.
func (m *Moonraker) SetTemps(extruder, bed float64) {
	m.with(func() { m.extruder, m.bed = extruder, bed })
}

// SetSpoolMaterial sets the Spoolman active spool's material ("" = none).
func (m *Moonraker) SetSpoolMaterial(s string) { m.with(func() { m.spoolMaterial = s }) }

// SetFileFilament sets filament_type reported in file metadata.
func (m *Moonraker) SetFileFilament(s string) { m.with(func() { m.fileFilament = s }) }

// Tick advances a running print by 25 %; at 100 % the print completes.
func (m *Moonraker) Tick() {
	m.with(func() {
		if m.state != "printing" {
			return
		}
		m.progress += 0.25
		if m.progress >= 1 {
			m.progress, m.state = 1, "complete"
		}
	})
}

// Files returns a copy of the uploaded files.
func (m *Moonraker) Files() map[string][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string][]byte, len(m.files))
	for k, v := range m.files {
		out[k] = append([]byte(nil), v...)
	}
	return out
}

// Calls returns the control calls received (upload, start, pause, ...).
func (m *Moonraker) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

// State returns print_stats.state and the current file.
func (m *Moonraker) State() (string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, m.file
}

func (m *Moonraker) with(f func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f()
}

func (m *Moonraker) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		down := m.down
		m.mu.Unlock()
		if down {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if m.APIKey != "" && r.Header.Get("X-Api-Key") != m.APIKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (m *Moonraker) handleInfo(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{"result": map[string]interface{}{"klippy_state": m.klippy}})
}

func (m *Moonraker) statusLocked(now time.Time) map[string]interface{} {
	pos, speed, commanded, homed := m.motionLocked(now)
	return map[string]interface{}{
		"webhooks":       map[string]interface{}{"state": m.klippy},
		"extruder":       map[string]interface{}{"temperature": m.extruder, "target": 0.0},
		"heater_bed":     map[string]interface{}{"temperature": m.bed, "target": 0.0},
		"print_stats":    map[string]interface{}{"state": m.state, "filename": m.file, "print_duration": 0.0},
		"display_status": map[string]interface{}{"progress": m.progress},
		"toolhead":       map[string]interface{}{"position": coordList(commanded), "homed_axes": homed},
		"motion_report":  map[string]interface{}{"live_position": coordList(pos), "live_velocity": speed, "live_extruder_velocity": 0.0},
	}
}

// coordList encodes a coordinate as Klipper does: a JSON array [x, y, z, e].
func coordList(c [4]float64) []interface{} { return []interface{}{c[0], c[1], c[2], c[3]} }

// SetMotionCadence changes the notification interval (default DefaultMotionCadence).
func (m *Moonraker) SetMotionCadence(d time.Duration) { m.with(func() { m.cadence = d }) }

// PlayGCode parses src and starts moving the simulated toolhead along it from
// the current position now. No machine is involved.
func (m *Moonraker) PlayGCode(src []byte) (*GCodePath, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	start, _, _, homed := m.motionLocked(now)
	p, err := ParseGCodePath(src, start)
	if err != nil {
		return nil, err
	}
	m.rest, m.homed = start, homed
	m.path, m.pathStart = p, now
	return p, nil
}

// Emissions returns every live_position the simulator has sent, in order.
func (m *Moonraker) Emissions() []MotionEmission {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]MotionEmission(nil), m.emissions...)
}

// motionLocked returns the toolhead state at now. Caller holds m.mu.
func (m *Moonraker) motionLocked(now time.Time) (pos [4]float64, speed float64, commanded [4]float64, homed string) {
	if m.path == nil {
		return m.rest, 0, m.rest, m.homed
	}
	t := now.Sub(m.pathStart)
	pos, speed, commanded = m.path.At(t)
	homed = mergeHomed(m.homed, m.path.HomedAt(t))
	return pos, speed, commanded, homed
}

func mergeHomed(a, b string) string {
	out := ""
	for _, axis := range "xyz" {
		for _, s := range []string{a, b} {
			if containsRune(s, axis) {
				out += string(axis)
				break
			}
		}
	}
	return out
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func (m *Moonraker) handleQuery(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{"result": map[string]interface{}{"status": m.statusLocked(time.Now())}})
}

func (m *Moonraker) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if root := r.FormValue("root"); root != "" && root != "gcodes" {
		http.Error(w, "unsupported root", http.StatusBadRequest)
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m.with(func() {
		m.files[hdr.Filename] = data
		m.calls = append(m.calls, "upload "+hdr.Filename)
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{"result": map[string]interface{}{"item": map[string]string{"path": hdr.Filename, "root": "gcodes"}, "action": "create_file"}})
}

func (m *Moonraker) handleStart(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("filename")
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[name]; !ok {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": map[string]string{"message": "file not found"}})
		return
	}
	if m.state == "printing" || m.state == "paused" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": map[string]string{"message": "printer busy"}})
		return
	}
	m.calls = append(m.calls, "start "+name)
	m.state, m.file, m.progress = "printing", name, 0
	writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

func (m *Moonraker) transition(call, from, to string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.calls = append(m.calls, call)
		if from != "" && m.state != from {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": map[string]string{"message": "invalid state " + m.state}})
			return
		}
		if to == "cancelled" && m.state != "printing" && m.state != "paused" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": map[string]string{"message": "no print to cancel"}})
			return
		}
		m.state = to
		writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
	}
}

func (m *Moonraker) handleScript(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Script string `json:"script"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	m.with(func() { m.calls = append(m.calls, "gcode "+body.Script) })
	writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

func (m *Moonraker) handleSpoolID(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var id interface{}
	if m.spoolMaterial != "" {
		id = 7
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"result": map[string]interface{}{"spool_id": id}})
}

func (m *Moonraker) handleSpoolProxy(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{"result": map[string]interface{}{
		"id": 7, "filament": map[string]string{"material": m.spoolMaterial},
	}})
}

func (m *Moonraker) handleMetadata(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[r.URL.Query().Get("filename")]; !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"result": map[string]string{"filament_type": m.fileFilament}})
}

var upgrader = websocket.Upgrader{}

// wsClient is one WebSocket connection and its subscription.
type wsClient struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	mu      sync.Mutex
	objects map[string][]string // nil field list = every field
	last    map[string]map[string]interface{}
}

func (c *wsClient) write(v interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}

// handleWebsocket answers printer.objects.subscribe with the subscribed
// objects, then sends notify_status_update with the changed fields every
// cadence, as Klipper and Moonraker do.
func (m *Moonraker) handleWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	c := &wsClient{conn: conn}
	done := make(chan struct{})
	defer close(done)
	go m.notifyLoop(c, done)
	for {
		var msg struct {
			Method string `json:"method"`
			ID     int64  `json:"id"`
			Params struct {
				Objects map[string][]string `json:"objects"`
			} `json:"params"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		m.mu.Lock()
		now := time.Now()
		status := filterStatus(m.statusLocked(now), msg.Params.Objects)
		eventtime := now.Sub(m.clockZero).Seconds()
		m.recordLocked(status, now)
		m.mu.Unlock()
		c.mu.Lock()
		c.objects, c.last = msg.Params.Objects, status
		c.mu.Unlock()
		reply := map[string]interface{}{"jsonrpc": "2.0", "id": msg.ID,
			"result": map[string]interface{}{"eventtime": eventtime, "status": status}}
		if err := c.write(reply); err != nil {
			return
		}
	}
}

// notifyLoop sends the changed subscribed fields every cadence.
func (m *Moonraker) notifyLoop(c *wsClient, done <-chan struct{}) {
	for {
		m.mu.Lock()
		cadence := m.cadence
		m.mu.Unlock()
		select {
		case <-done:
			return
		case <-time.After(cadence):
		}
		c.mu.Lock()
		objects := c.objects
		c.mu.Unlock()
		if objects == nil {
			continue
		}
		m.mu.Lock()
		now := time.Now()
		status := filterStatus(m.statusLocked(now), objects)
		eventtime := now.Sub(m.clockZero).Seconds()
		c.mu.Lock()
		diff := diffStatus(c.last, status)
		c.last = status
		c.mu.Unlock()
		if len(diff) > 0 {
			m.recordLocked(diff, now)
		}
		m.mu.Unlock()
		if len(diff) == 0 {
			continue
		}
		if err := c.write(map[string]interface{}{"jsonrpc": "2.0", "method": "notify_status_update",
			"params": []interface{}{diff, eventtime}}); err != nil {
			return
		}
	}
}

// recordLocked logs a live_position being sent. Caller holds m.mu.
func (m *Moonraker) recordLocked(status map[string]map[string]interface{}, now time.Time) {
	mr, ok := status["motion_report"]
	if !ok {
		return
	}
	lp, ok := mr["live_position"].([]interface{})
	if !ok {
		return
	}
	_, speed, _, homed := m.motionLocked(now)
	var pos [4]float64
	for i := range pos {
		pos[i], _ = lp[i].(float64)
	}
	m.emissions = append(m.emissions, MotionEmission{At: now, Position: pos, Velocity: speed, Homed: homed})
}

// filterStatus keeps the subscribed objects and fields (an unknown object
// yields an empty object, as Klipper answers).
func filterStatus(all map[string]interface{}, objects map[string][]string) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	for name, fields := range objects {
		obj, _ := all[name].(map[string]interface{})
		sel := map[string]interface{}{}
		for k, v := range obj {
			if fields == nil || containsString(fields, k) {
				sel[k] = v
			}
		}
		out[name] = sel
	}
	return out
}

// diffStatus returns the fields of next that differ from prev.
func diffStatus(prev, next map[string]map[string]interface{}) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	for name, obj := range next {
		for k, v := range obj {
			if old, ok := prev[name][k]; ok && reflect.DeepEqual(old, v) {
				continue
			}
			if out[name] == nil {
				out[name] = map[string]interface{}{}
			}
			out[name][k] = v
		}
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
