package adapters

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// FTPSUpload stores r as name on an implicit-TLS FTP server (Bambu Lab LAN
// mode exposes one on port 990, user "bblp", password = the LAN access code).
// The data connection is protected (PROT P) and reuses the control session's
// TLS configuration, including its session cache, because printers commonly
// require TLS session resumption on the data channel.
func FTPSUpload(ctx context.Context, addr, user, password string, tlsCfg *tls.Config, name string, r io.Reader) error {
	if strings.ContainsAny(name, "/\\\r\n") || name == "" {
		return fmt.Errorf("ftps: invalid remote file name %q", name)
	}
	cfg := tlsCfg.Clone()
	if cfg.ClientSessionCache == nil {
		cfg.ClientSessionCache = tls.NewLRUClientSessionCache(4)
	}
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: cfg}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("ftps: connect: %w", err)
	}
	defer func() { _ = raw.Close() }()
	if dl, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(dl)
	}
	conn := textproto.NewConn(raw)

	if _, _, err := conn.ReadResponse(220); err != nil {
		return fmt.Errorf("ftps: greeting: %w", err)
	}
	steps := []struct {
		cmd    string
		expect int
	}{
		{"USER " + user, 331},
		{"PASS " + password, 230},
		{"PBSZ 0", 200},
		{"PROT P", 200},
		{"TYPE I", 200},
	}
	for _, s := range steps {
		if err := ftpCmd(conn, s.expect, s.cmd); err != nil {
			if strings.HasPrefix(s.cmd, "PASS ") {
				return fmt.Errorf("ftps: login rejected: %w", err)
			}
			return err
		}
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("ftps: address: %w", err)
	}
	dataPort, err := ftpPassive(conn)
	if err != nil {
		return err
	}
	dataAddr := net.JoinHostPort(host, strconv.Itoa(dataPort)) // ignore the PASV host; use the control host
	dataRaw, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", dataAddr)
	if err != nil {
		return fmt.Errorf("ftps: data connect: %w", err)
	}
	if err := ftpCmd(conn, 150, "STOR "+name); err != nil {
		_ = dataRaw.Close()
		return err
	}
	data := tls.Client(dataRaw, cfg)
	if dl, ok := ctx.Deadline(); ok {
		_ = data.SetDeadline(dl)
	}
	if _, err := io.Copy(data, r); err != nil {
		_ = data.Close()
		return fmt.Errorf("ftps: transfer: %w", err)
	}
	if err := data.Close(); err != nil {
		return fmt.Errorf("ftps: close data: %w", err)
	}
	if _, _, err := conn.ReadResponse(226); err != nil {
		return fmt.Errorf("ftps: transfer not confirmed: %w", err)
	}
	_ = conn.PrintfLine("QUIT")
	return nil
}

func ftpCmd(conn *textproto.Conn, expect int, cmd string) error {
	if err := conn.PrintfLine("%s", cmd); err != nil {
		return fmt.Errorf("ftps: send: %w", err)
	}
	if _, _, err := conn.ReadResponse(expect); err != nil {
		verb, _, _ := strings.Cut(cmd, " ")
		return fmt.Errorf("ftps: %s: %w", verb, err)
	}
	return nil
}

// ftpPassive issues PASV and returns the data port.
func ftpPassive(conn *textproto.Conn) (int, error) {
	if err := conn.PrintfLine("PASV"); err != nil {
		return 0, fmt.Errorf("ftps: send PASV: %w", err)
	}
	_, msg, err := conn.ReadResponse(227)
	if err != nil {
		return 0, fmt.Errorf("ftps: PASV: %w", err)
	}
	start, end := strings.Index(msg, "("), strings.Index(msg, ")")
	if start < 0 || end < start {
		return 0, fmt.Errorf("ftps: malformed PASV reply %q", msg)
	}
	parts := strings.Split(msg[start+1:end], ",")
	if len(parts) != 6 {
		return 0, fmt.Errorf("ftps: malformed PASV reply %q", msg)
	}
	hi, err1 := strconv.Atoi(strings.TrimSpace(parts[4]))
	lo, err2 := strconv.Atoi(strings.TrimSpace(parts[5]))
	if err1 != nil || err2 != nil || hi < 0 || hi > 255 || lo < 0 || lo > 255 {
		return 0, fmt.Errorf("ftps: malformed PASV port in %q", msg)
	}
	return hi*256 + lo, nil
}
