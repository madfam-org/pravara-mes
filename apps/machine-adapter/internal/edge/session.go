package edge

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

// errHostOffline ends a session when the primary host reports offline.
var errHostOffline = errors.New("primary host application went offline")

// dial opens one MQTT session whose will is the NDEATH for bdSeq (spec 3.0:
// will QoS 1, not retained). Automatic reconnect is off because every new
// CONNECT needs a new bdSeq in its will.
func (n *Node) dial(bdSeq uint64) (paho.Client, <-chan error, error) {
	death, err := sparkplug.NodeDeath(bdSeq, n.now())
	if err != nil {
		return nil, nil, err
	}
	willPayload, err := sparkplug.Encode(death)
	if err != nil {
		return nil, nil, err
	}
	willTopic, err := sparkplug.NodeTopic(n.cfg.GroupID, sparkplug.NDEATH, n.cfg.EdgeNodeID)
	if err != nil {
		return nil, nil, err
	}

	opts := paho.NewClientOptions().
		AddBroker(n.cfg.BrokerURL).
		SetClientID(n.cfg.ClientID).
		SetCleanSession(true).
		SetAutoReconnect(false).
		SetConnectRetry(false).
		SetKeepAlive(30*time.Second).
		SetConnectTimeout(15*time.Second).
		SetOrderMatters(false).
		SetBinaryWill(willTopic, willPayload, 1, false)
	if n.cfg.Username != "" {
		opts.SetUsername(n.cfg.Username)
		opts.SetPassword(n.password)
	}
	if n.tlsConfig != nil {
		opts.SetTLSConfig(n.tlsConfig)
	}
	lost := make(chan error, 1)
	opts.SetConnectionLostHandler(func(_ paho.Client, err error) {
		select {
		case lost <- err:
		default:
		}
	})

	client := paho.NewClient(opts)
	tok := client.Connect()
	if !tok.WaitTimeout(20 * time.Second) {
		client.Disconnect(0)
		return nil, nil, fmt.Errorf("broker connect timed out")
	}
	if err := tok.Error(); err != nil {
		return nil, nil, fmt.Errorf("broker connect: %w", err)
	}
	return client, lost, nil
}

// subscribe registers the edge node's subscriptions (before NBIRTH, QoS 1):
// its NCMD topic, its devices' DCMD topics and the primary host's STATE.
func (n *Node) subscribe(client paho.Client) error {
	ncmd, _ := sparkplug.NodeTopic(n.cfg.GroupID, sparkplug.NCMD, n.cfg.EdgeNodeID)
	subs := map[string]paho.MessageHandler{
		ncmd: n.onNCMD,
		sparkplug.Namespace + "/" + n.cfg.GroupID + "/" + string(sparkplug.DCMD) + "/" + n.cfg.EdgeNodeID + "/+": n.onDCMD,
	}
	if n.cfg.WaitsForPrimaryHost() {
		state, err := sparkplug.StateTopic(n.cfg.PrimaryHostID)
		if err != nil {
			return err
		}
		subs[state] = n.onSTATE
	}
	for topic, h := range subs {
		tok := client.Subscribe(topic, 1, h)
		if !tok.WaitTimeout(10 * time.Second) {
			return fmt.Errorf("subscribe %s timed out", topic)
		}
		if err := tok.Error(); err != nil {
			return fmt.Errorf("subscribe %s: %w", topic, err)
		}
	}
	return nil
}

// buildTLSConfig returns nil for plaintext brokers (validated loopback only).
func buildTLSConfig(cfg Config) (*tls.Config, error) {
	u, err := url.Parse(cfg.BrokerURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "tcp" || u.Scheme == "mqtt" {
		return nil, nil
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.TLSServerName}
	if cfg.TLSCAFile != "" {
		pem, err := os.ReadFile(cfg.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("read broker CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("broker CA file holds no PEM certificate")
		}
		tc.RootCAs = pool
	}
	if cfg.TLSClientCertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSClientCertFile, cfg.TLSClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		tc.Certificates = []tls.Certificate{cert}
	}
	return tc, nil
}

// bdSeqStore persists the last bdSeq so a restarted edge node continues the
// sequence (the host uses it to pair NBIRTH and NDEATH).
type bdSeqStore struct {
	mu   sync.Mutex
	path string // "" = memory only
	prev uint64
	has  bool
}

func loadBdSeq(dir string) *bdSeqStore {
	s := &bdSeqStore{}
	if dir == "" {
		return s
	}
	s.path = filepath.Join(dir, "bdseq")
	if b, err := os.ReadFile(s.path); err == nil {
		if v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 8); err == nil {
			s.prev, s.has = v, true
		}
	}
	return s
}

// Next returns the bdSeq for the next CONNECT and persists it.
func (s *bdSeqStore) Next() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := uint64(0)
	if s.has {
		v = sparkplug.NextBdSeq(s.prev)
	}
	s.prev, s.has = v, true
	if s.path == "" {
		return v, nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return v, err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatUint(v, 10)+"\n"), 0o600); err != nil {
		return v, err
	}
	return v, os.Rename(tmp, s.path)
}
