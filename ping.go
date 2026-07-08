package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
	"golang.org/x/net/proxy"
)

// endpointOf آدرس و پورت سرور یک لینک را استخراج می‌کند.
func endpointOf(link string) (string, int, error) {
	if strings.HasPrefix(strings.TrimSpace(link), "ssh://") {
		c, err := parseSSHLink(link)
		if err != nil {
			return "", 0, err
		}
		return c.host, c.port, nil
	}
	out, err := parseLink(link)
	if err != nil {
		return "", 0, err
	}
	settings, ok := out["settings"].(map[string]interface{})
	if !ok {
		return "", 0, fmt.Errorf("no settings")
	}
	if vnext, ok := settings["vnext"].([]map[string]interface{}); ok && len(vnext) > 0 {
		return asString(vnext[0]["address"]), asInt(vnext[0]["port"]), nil
	}
	if servers, ok := settings["servers"].([]map[string]interface{}); ok && len(servers) > 0 {
		return asString(servers[0]["address"]), asInt(servers[0]["port"]), nil
	}
	return "", 0, fmt.Errorf("no endpoint")
}

func asString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func asInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

// tcpPing زمان برقراری اتصال TCP به سرور کانفیگ را اندازه می‌گیرد.
func tcpPing(link string, timeout time.Duration) (time.Duration, error) {
	host, port, err := endpointOf(link)
	if err != nil {
		return 0, err
	}
	if host == "" || port == 0 {
		return 0, fmt.Errorf("invalid endpoint")
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return 0, err
	}
	conn.Close()
	return time.Since(start), nil
}

// pickFreePort یک پورت TCP آزاد روی لوکال‌هاست پیدا می‌کند.
func pickFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// realPing برای این کانفیگ یک نمونهٔ موقتِ Xray بالا می‌آورد و از داخلِ پروکسی
// یک درخواست HTTP واقعی می‌زند تا پینگِ end-to-end واقعی را اندازه بگیرد.
// برخلاف tcpPing (که فقط reachability پورت — اغلب edge یک CDN — را می‌سنجد)،
// این تابع کانفیگ‌های مرده/نامعتبر را درست تشخیص می‌دهد و پینگِ راستین می‌دهد.
func realPing(link string, timeout time.Duration) (time.Duration, error) {
	link = strings.TrimSpace(link)

	var outbound map[string]interface{}
	var tun *sshTunnel
	if strings.HasPrefix(link, "ssh://") {
		t, sshPort, err := startSSHTunnel(link)
		if err != nil {
			return 0, fmt.Errorf("ssh tunnel: %w", err)
		}
		tun = t
		outbound = sshOutbound(sshPort)
	} else {
		ob, err := parseLink(link)
		if err != nil {
			return 0, err
		}
		outbound = ob
	}
	defer func() {
		if tun != nil {
			tun.Close()
		}
	}()

	port, err := pickFreePort()
	if err != nil {
		return 0, err
	}

	cfg := buildConfig("127.0.0.1", port, 0, outbound, nil)
	jsonBytes, _ := json.MarshalIndent(cfg, "", "  ")
	coreCfg, err := serial.LoadJSONConfig(bytes.NewReader(jsonBytes))
	if err != nil {
		return 0, fmt.Errorf("load config: %w", err)
	}
	inst, err := core.New(coreCfg)
	if err != nil {
		return 0, fmt.Errorf("create core: %w", err)
	}
	if err := inst.Start(); err != nil {
		return 0, fmt.Errorf("start core: %w", err)
	}
	defer inst.Close()

	return proxyHealth("127.0.0.1", port, timeout)
}

// proxyHealth از طریق پروکسی لوکال یک درخواست واقعی می‌زند تا سلامت اتصال را بسنجد.
func proxyHealth(listen string, socks int, timeout time.Duration) (time.Duration, error) {
	socksAddr := net.JoinHostPort(listen, fmt.Sprintf("%d", socks))
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, &net.Dialer{Timeout: timeout})
	if err != nil {
		return 0, err
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
	}
	client := &http.Client{Transport: transport, Timeout: timeout}

	start := time.Now()
	resp, err := client.Get("http://cp.cloudflare.com/generate_204")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	return time.Since(start), nil
}
