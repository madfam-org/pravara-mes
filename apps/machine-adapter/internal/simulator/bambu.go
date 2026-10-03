package simulator

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

// Bambu simulates a Bambu Lab printer in LAN mode: its local MQTT broker on
// TLS (user "bblp", password = access code) and its implicit-FTPS server.
type Bambu struct {
	Serial     string
	AccessCode string

	cert     *Cert
	server   *mqtt.Server
	ftps     *ftpsServer
	mqttPort int

	mu        sync.Mutex
	state     string // gcode_state
	nozzle    float64
	bed       float64
	percent   int
	subtask   string
	trays     []string
	external  string
	files     map[string][]byte
	commands  []string
	lastStart map[string]interface{}
}

// NewBambu returns a stopped simulator.
func NewBambu(serial, accessCode string) *Bambu {
	return &Bambu{Serial: serial, AccessCode: accessCode, state: "IDLE", nozzle: 25, bed: 24, files: map[string][]byte{}}
}

// Start opens the MQTT (TLS) and FTPS listeners on loopback.
func (b *Bambu) Start() error {
	cert, err := SelfSigned("bambu-simulator")
	if err != nil {
		return err
	}
	b.cert = cert
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert.TLS}, MinVersion: tls.VersionTLS12}

	b.mqttPort, err = FreePort()
	if err != nil {
		return err
	}
	b.server = mqtt.New(&mqtt.Options{InlineClient: true, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := b.server.AddHook(&accessCodeHook{code: b.AccessCode}, nil); err != nil {
		return err
	}
	l := listeners.NewTCP(listeners.Config{ID: "bambu-lan", Address: fmt.Sprintf("127.0.0.1:%d", b.mqttPort), TLSConfig: tlsCfg})
	if err := b.server.AddListener(l); err != nil {
		return err
	}
	if err := b.server.Serve(); err != nil {
		return err
	}
	if err := b.server.Subscribe("device/"+b.Serial+"/request", 1, b.onRequest); err != nil {
		return err
	}
	b.ftps, err = startFTPS(tlsCfg, "bblp", b.AccessCode, b.storeFile)
	return err
}

// Close stops both listeners.
func (b *Bambu) Close() {
	if b.ftps != nil {
		b.ftps.Close()
	}
	if b.server != nil {
		_ = b.server.Close()
	}
}

// Host returns the loopback host.
func (b *Bambu) Host() string { return "127.0.0.1" }

// Ports returns the MQTT and FTPS ports.
func (b *Bambu) Ports() (int, int) { return b.mqttPort, b.ftps.port }

// CertSHA256 is the pin of the simulator's certificate.
func (b *Bambu) CertSHA256() string { return b.cert.SHA256 }

// SetTrays sets the AMS tray_type values ("" = empty tray).
func (b *Bambu) SetTrays(types ...string) {
	b.mu.Lock()
	b.trays = append([]string(nil), types...)
	b.mu.Unlock()
	b.report(true)
}

// SetExternalSpool sets the external spool's tray_type.
func (b *Bambu) SetExternalSpool(t string) {
	b.mu.Lock()
	b.external = t
	b.mu.Unlock()
	b.report(true)
}

// SetTemps publishes new nozzle and bed temperatures (incremental report).
func (b *Bambu) SetTemps(nozzle, bed float64) {
	b.mu.Lock()
	b.nozzle, b.bed = nozzle, bed
	b.mu.Unlock()
	b.publish(map[string]interface{}{"nozzle_temper": nozzle, "bed_temper": bed})
}

// Tick advances a running print by 25 %; at 100 % it reports FINISH.
func (b *Bambu) Tick() {
	b.mu.Lock()
	if b.state != "RUNNING" {
		b.mu.Unlock()
		return
	}
	b.percent += 25
	if b.percent >= 100 {
		b.percent, b.state = 100, "FINISH"
	}
	fields := map[string]interface{}{"mc_percent": b.percent, "gcode_state": b.state}
	b.mu.Unlock()
	b.publish(fields)
}

// Files returns a copy of the files uploaded over FTPS.
func (b *Bambu) Files() map[string][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[string][]byte{}
	for k, v := range b.files {
		out[k] = append([]byte(nil), v...)
	}
	return out
}

// Commands returns the "print.command" values received.
func (b *Bambu) Commands() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.commands...)
}

// LastProjectFile returns the last project_file request.
func (b *Bambu) LastProjectFile() map[string]interface{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastStart
}

// State returns gcode_state.
func (b *Bambu) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Bambu) storeFile(name string, data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.files[name] = data
}

func (b *Bambu) onRequest(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
	var req struct {
		Print map[string]interface{} `json:"print"`
	}
	if json.Unmarshal(pk.Payload, &req) != nil || req.Print == nil {
		return
	}
	cmd, _ := req.Print["command"].(string)
	b.mu.Lock()
	b.commands = append(b.commands, cmd)
	var fields map[string]interface{}
	full := false
	switch cmd {
	case "push_status":
		full = true
	case "pause":
		if b.state == "RUNNING" {
			b.state = "PAUSE"
		}
	case "resume":
		if b.state == "PAUSE" {
			b.state = "RUNNING"
		}
	case "stop":
		if b.state == "RUNNING" || b.state == "PAUSE" {
			b.state = "FAILED"
		}
	case "project_file":
		b.lastStart = req.Print
		url, _ := req.Print["url"].(string)
		name := url[strings.LastIndex(url, "/")+1:]
		if _, ok := b.files[name]; ok && strings.HasPrefix(url, "file:///sdcard/") {
			b.state, b.percent = "RUNNING", 0
			b.subtask, _ = req.Print["subtask_name"].(string)
		} else {
			b.state = "FAILED"
		}
	}
	if !full {
		fields = map[string]interface{}{"gcode_state": b.state, "mc_percent": b.percent, "subtask_name": b.subtask}
	}
	b.mu.Unlock()
	if full {
		b.report(true)
		return
	}
	b.publish(fields)
}

// report publishes a full status report.
func (b *Bambu) report(_ bool) {
	if b.server == nil {
		return
	}
	b.mu.Lock()
	var trays []map[string]string
	for i, t := range b.trays {
		trays = append(trays, map[string]string{"id": fmt.Sprint(i), "tray_type": t})
	}
	fields := map[string]interface{}{
		"gcode_state": b.state, "nozzle_temper": b.nozzle, "bed_temper": b.bed,
		"mc_percent": b.percent, "subtask_name": b.subtask,
	}
	if trays != nil {
		fields["ams"] = map[string]interface{}{"ams": []map[string]interface{}{{"id": "0", "humidity": "4", "tray": trays}}}
	}
	if b.external != "" {
		fields["vt_tray"] = map[string]string{"tray_type": b.external}
	}
	b.mu.Unlock()
	b.publish(fields)
}

func (b *Bambu) publish(fields map[string]interface{}) {
	if b.server == nil {
		return
	}
	fields["command"] = "push_status"
	body, _ := json.Marshal(map[string]interface{}{"print": fields})
	_ = b.server.Publish("device/"+b.Serial+"/report", body, false, 0)
}

// accessCodeHook authenticates "bblp" with the access code and allows all topics.
type accessCodeHook struct {
	mqtt.HookBase
	code string
}

func (h *accessCodeHook) ID() string { return "bambu-access-code" }

func (h *accessCodeHook) Provides(b byte) bool {
	return bytes.Contains([]byte{mqtt.OnConnectAuthenticate, mqtt.OnACLCheck}, []byte{b})
}

func (h *accessCodeHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
	return string(pk.Connect.Username) == "bblp" && string(pk.Connect.Password) == h.code
}

func (h *accessCodeHook) OnACLCheck(*mqtt.Client, string, bool) bool { return true }

// ftpsServer is a minimal implicit-TLS FTP server accepting STOR uploads.
type ftpsServer struct {
	ln    net.Listener
	port  int
	tls   *tls.Config
	user  string
	pass  string
	store func(string, []byte)
	wg    sync.WaitGroup
}

func startFTPS(cfg *tls.Config, user, pass string, store func(string, []byte)) (*ftpsServer, error) {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		return nil, err
	}
	s := &ftpsServer{ln: ln, port: ln.Addr().(*net.TCPAddr).Port, tls: cfg, user: user, pass: pass, store: store}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

// Close stops accepting and waits for open sessions.
func (s *ftpsServer) Close() {
	_ = s.ln.Close()
	s.wg.Wait()
}

func (s *ftpsServer) serve() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.session(c)
		}()
	}
}

func (s *ftpsServer) session(c net.Conn) {
	defer c.Close()
	w := func(line string) { _, _ = io.WriteString(c, line+"\r\n") }
	w("220 simulator ready")
	var buf bytes.Buffer
	one := make([]byte, 1)
	readLine := func() (string, bool) {
		buf.Reset()
		for {
			if _, err := c.Read(one); err != nil {
				return "", false
			}
			if one[0] == '\n' {
				return strings.TrimRight(buf.String(), "\r"), true
			}
			buf.WriteByte(one[0])
		}
	}
	user, authed := "", false
	var pasv net.Listener
	defer func() {
		if pasv != nil {
			pasv.Close()
		}
	}()
	for {
		line, ok := readLine()
		if !ok {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "USER":
			user = arg
			w("331 password required")
		case "PASS":
			if user == s.user && arg == s.pass {
				authed = true
				w("230 logged in")
			} else {
				w("530 login incorrect")
			}
		case "PBSZ", "PROT", "TYPE":
			w("200 ok")
		case "PASV":
			if !authed {
				w("530 not logged in")
				continue
			}
			l, err := tls.Listen("tcp", "127.0.0.1:0", s.tls)
			if err != nil {
				w("425 cannot open data connection")
				continue
			}
			pasv = l
			p := l.Addr().(*net.TCPAddr).Port
			w(fmt.Sprintf("227 Entering Passive Mode (127,0,0,1,%d,%d)", p/256, p%256))
		case "STOR":
			if !authed || pasv == nil {
				w("425 use PASV first")
				continue
			}
			w("150 opening data connection")
			dc, err := pasv.Accept()
			if err != nil {
				w("425 data connection failed")
				continue
			}
			data, err := io.ReadAll(dc)
			dc.Close()
			pasv.Close()
			pasv = nil
			if err != nil {
				w("451 transfer aborted")
				continue
			}
			s.store(arg, data)
			w("226 transfer complete")
		case "QUIT":
			w("221 bye")
			return
		default:
			w("502 not implemented")
		}
	}
}
