package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

// endpointOf آدرس و پورت سرور یک لینک را استخراج می‌کند.
func endpointOf(link string) (string, int, error) {
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
