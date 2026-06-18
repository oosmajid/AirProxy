package main

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshConfig پارامترهای یک تونل SSH را نگه می‌دارد.
type sshConfig struct {
	host       string
	port       int
	user       string
	password   string
	privateKey []byte
	passphrase string
}

// parseSSHLink یک لینک ssh:// را به sshConfig تبدیل می‌کند.
// فرمت: ssh://user:password@host:port  یا  ssh://user@host:port?pk=<base64>&pass=<base64>
// همچنین ?keyfile=/path/to/key و ?password=... پشتیبانی می‌شوند.
func parseSSHLink(raw string) (*sshConfig, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ssh" {
		return nil, fmt.Errorf("لینک ssh نامعتبر")
	}
	c := &sshConfig{
		host: u.Hostname(),
		user: u.User.Username(),
	}
	if c.host == "" {
		return nil, fmt.Errorf("ssh: هاست مشخص نشده")
	}
	c.port = atoi(u.Port())
	if c.port == 0 {
		c.port = 22
	}
	if pw, ok := u.User.Password(); ok {
		c.password = pw
	}

	q := u.Query()
	if pk := q.Get("pk"); pk != "" {
		if dec, err := b64decode(pk); err == nil {
			c.privateKey = []byte(dec)
		} else {
			c.privateKey = []byte(pk)
		}
	}
	if kf := q.Get("keyfile"); kf != "" && len(c.privateKey) == 0 {
		if b, err := os.ReadFile(kf); err == nil {
			c.privateKey = b
		} else {
			return nil, fmt.Errorf("خواندن فایل کلید: %w", err)
		}
	}
	if ps := q.Get("pass"); ps != "" {
		if dec, err := b64decode(ps); err == nil {
			c.passphrase = dec
		} else {
			c.passphrase = ps
		}
	}
	if c.password == "" {
		c.password = q.Get("password")
	}
	return c, nil
}

// clientConfig روش‌های احراز هویت را بر اساس پسورد/کلید می‌سازد.
func (c *sshConfig) clientConfig() (*ssh.ClientConfig, error) {
	var auth []ssh.AuthMethod
	if len(c.privateKey) > 0 {
		var signer ssh.Signer
		var err error
		if c.passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(c.privateKey, []byte(c.passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(c.privateKey)
		}
		if err != nil {
			return nil, fmt.Errorf("کلید خصوصی نامعتبر: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if c.password != "" {
		auth = append(auth, ssh.Password(c.password))
		// برخی سرورها پسورد را به‌صورت keyboard-interactive می‌خواهند.
		auth = append(auth, ssh.KeyboardInteractive(
			func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				ans := make([]string, len(questions))
				for i := range questions {
					ans[i] = c.password
				}
				return ans, nil
			}))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("ssh: روش احراز هویت لازم است (پسورد یا کلید خصوصی)")
	}
	return &ssh.ClientConfig{
		User:            c.user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}, nil
}

// sshTunnel یک اتصال SSH باز را نگه می‌دارد و یک SOCKS5 محلی (فقط CONNECT/TCP)
// ارائه می‌دهد که ترافیک را از داخل تونل SSH عبور می‌دهد.
type sshTunnel struct {
	mu       sync.Mutex
	client   *ssh.Client
	listener net.Listener
	done     chan struct{}
}

// sshOutbound یک اوت‌باند socks برای xray می‌سازد که به SOCKS5 محلیِ تونل اشاره می‌کند.
func sshOutbound(port int) map[string]interface{} {
	return map[string]interface{}{
		"tag":      "proxy",
		"protocol": "socks",
		"settings": map[string]interface{}{
			"servers": []map[string]interface{}{
				{"address": "127.0.0.1", "port": port},
			},
		},
	}
}

// startSSHTunnel به سرور SSH وصل می‌شود و یک SOCKS5 محلی روی پورت آزاد بالا می‌آورد.
// پورت محلیِ انتخاب‌شده را برمی‌گرداند.
func startSSHTunnel(raw string) (*sshTunnel, int, error) {
	cfg, err := parseSSHLink(raw)
	if err != nil {
		return nil, 0, err
	}
	clientCfg, err := cfg.clientConfig()
	if err != nil {
		return nil, 0, err
	}
	addr := net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port))
	client, err := ssh.Dial("tcp", addr, clientCfg)
	if err != nil {
		return nil, 0, fmt.Errorf("اتصال SSH: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		client.Close()
		return nil, 0, err
	}
	t := &sshTunnel{client: client, listener: ln, done: make(chan struct{})}
	port := ln.Addr().(*net.TCPAddr).Port
	go t.serve()
	go t.keepAlive()
	return t, port, nil
}

func (t *sshTunnel) serve() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			return // listener بسته شده
		}
		go t.handle(conn)
	}
}

func (t *sshTunnel) handle(conn net.Conn) {
	defer conn.Close()
	target, err := socks5Handshake(conn)
	if err != nil {
		return
	}
	remote, err := t.client.Dial("tcp", target)
	if err != nil {
		socksReply(conn, 0x05) // connection refused
		return
	}
	defer remote.Close()
	socksReply(conn, 0x00) // success

	done := make(chan struct{}, 2)
	go func() { io.Copy(remote, conn); done <- struct{}{} }()
	go func() { io.Copy(conn, remote); done <- struct{}{} }()
	<-done
}

// keepAlive به‌صورت دوره‌ای یک درخواست keepalive می‌فرستد تا اتصال زنده بماند.
func (t *sshTunnel) keepAlive() {
	tk := time.NewTicker(20 * time.Second)
	defer tk.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-tk.C:
			t.mu.Lock()
			c := t.client
			t.mu.Unlock()
			if c == nil {
				return
			}
			if _, _, err := c.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				return
			}
		}
	}
}

// Close تونل را می‌بندد.
func (t *sshTunnel) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.done:
	default:
		close(t.done)
	}
	if t.listener != nil {
		t.listener.Close()
		t.listener = nil
	}
	if t.client != nil {
		t.client.Close()
		t.client = nil
	}
}

// ---- SOCKS5 (حداقلی، فقط CONNECT) ----

// socks5Handshake دست‌دادن SOCKS5 را انجام داده و آدرس مقصد را برمی‌گرداند.
func socks5Handshake(conn net.Conn) (string, error) {
	buf := make([]byte, 262)
	// greeting: VER NMETHODS METHODS...
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return "", err
	}
	if buf[0] != 0x05 {
		return "", fmt.Errorf("socks: نسخهٔ نامعتبر")
	}
	n := int(buf[1])
	if _, err := io.ReadFull(conn, buf[:n]); err != nil {
		return "", err
	}
	// انتخاب روش: بدون احراز هویت
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return "", err
	}

	// request: VER CMD RSV ATYP ...
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return "", err
	}
	if buf[0] != 0x05 {
		return "", fmt.Errorf("socks: درخواست نامعتبر")
	}
	cmd, atyp := buf[1], buf[3]
	if cmd != 0x01 { // فقط CONNECT
		socksReply(conn, 0x07)
		return "", fmt.Errorf("socks: دستور پشتیبانی‌نشده %d", cmd)
	}

	var host string
	switch atyp {
	case 0x01: // IPv4
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return "", err
		}
		host = net.IP(buf[:4]).String()
	case 0x04: // IPv6
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return "", err
		}
		host = net.IP(buf[:16]).String()
	case 0x03: // domain
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return "", err
		}
		l := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:l]); err != nil {
			return "", err
		}
		host = string(buf[:l])
	default:
		socksReply(conn, 0x08)
		return "", fmt.Errorf("socks: نوع آدرس نامعتبر")
	}

	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return "", err
	}
	port := int(buf[0])<<8 | int(buf[1])
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// socksReply یک پاسخ SOCKS5 با کد داده‌شده می‌فرستد (BND.ADDR = 0.0.0.0:0).
func socksReply(conn net.Conn, rep byte) {
	conn.Write([]byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
}
