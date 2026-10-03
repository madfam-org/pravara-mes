// Package simulator provides local protocol simulators for the printers the
// edge node drives (Moonraker/Klipper and Bambu Lab LAN mode). They accept
// the same requests as the real devices and never move hardware; tests and
// the edge node's simulate mode use them in place of printers.
package simulator

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// Cert is a self-signed certificate for loopback test servers.
type Cert struct {
	TLS    tls.Certificate
	Pool   *x509.CertPool
	PEM    []byte // certificate PEM, usable as a CA file
	SHA256 string // hex SHA-256 of the leaf DER
}

// SelfSigned creates a certificate valid for the given DNS names and 127.0.0.1.
func SelfSigned(dnsNames ...string) (*Cert, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "pravara simulator"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	sum := sha256.Sum256(der)
	return &Cert{
		TLS:    tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf},
		Pool:   pool,
		PEM:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		SHA256: hex.EncodeToString(sum[:]),
	}, nil
}

// FreePort returns a currently free loopback TCP port.
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address %v", l.Addr())
	}
	return addr.Port, nil
}
