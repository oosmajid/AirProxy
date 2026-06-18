package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// parseLink یک لینک اشتراکی را به یک اوت‌باند Xray تبدیل می‌کند.
func parseLink(raw string) (map[string]interface{}, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, "vmess://"):
		return parseVmess(raw)
	case strings.HasPrefix(raw, "vless://"):
		return parseVless(raw)
	case strings.HasPrefix(raw, "trojan://"):
		return parseTrojan(raw)
	case strings.HasPrefix(raw, "ss://"):
		return parseShadowsocks(raw)
	default:
		return nil, fmt.Errorf("پروتکل پشتیبانی‌نشده: %.10s", raw)
	}
}

// protoOf زیرعنوان پروتکل/ترنسپورت یک لینک را برمی‌گرداند (مثل "VLESS · WS").
func protoOf(raw string) string {
	raw = strings.TrimSpace(raw)
	var proto, net string
	switch {
	case strings.HasPrefix(raw, "vmess://"):
		proto = "VMess"
		if js, err := b64decode(strings.TrimPrefix(raw, "vmess://")); err == nil {
			var v map[string]interface{}
			if json.Unmarshal([]byte(js), &v) == nil {
				if n, ok := v["net"].(string); ok {
					net = n
				}
			}
		}
	case strings.HasPrefix(raw, "vless://"):
		proto = "VLESS"
		if u, err := url.Parse(raw); err == nil {
			net = u.Query().Get("type")
		}
	case strings.HasPrefix(raw, "trojan://"):
		proto = "Trojan"
		if u, err := url.Parse(raw); err == nil {
			net = u.Query().Get("type")
		}
	case strings.HasPrefix(raw, "ss://"):
		proto = "Shadowsocks"
	case strings.HasPrefix(raw, "ssh://"):
		proto = "SSH"
	default:
		proto = "Unknown"
	}
	if net != "" && net != "tcp" {
		return proto + " · " + strings.ToUpper(net)
	}
	return proto
}

// linkName تلاش می‌کند نام/ریمارک خوانای یک لینک را استخراج کند.
func linkName(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "vmess://") {
		if js, err := b64decode(strings.TrimPrefix(raw, "vmess://")); err == nil {
			var v map[string]interface{}
			if json.Unmarshal([]byte(js), &v) == nil {
				if ps, ok := v["ps"].(string); ok && ps != "" {
					return ps
				}
				if add, ok := v["add"].(string); ok {
					return add
				}
			}
		}
	}
	if i := strings.Index(raw, "#"); i >= 0 {
		if name, err := url.QueryUnescape(raw[i+1:]); err == nil && name != "" {
			return name
		}
		return raw[i+1:]
	}
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return summarize(raw)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func b64decode(s string) (string, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("base64 نامعتبر")
}

// ---- vmess ----

func parseVmess(raw string) (map[string]interface{}, error) {
	payload := strings.TrimPrefix(raw, "vmess://")
	jsonStr, err := b64decode(payload)
	if err != nil {
		return nil, fmt.Errorf("vmess base64: %w", err)
	}
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return nil, fmt.Errorf("vmess json: %w", err)
	}

	get := func(k string) string { return fmt.Sprintf("%v", v[k]) }
	port := atoi(get("port"))
	aid := 0
	if a, ok := v["aid"]; ok {
		aid = atoi(fmt.Sprintf("%v", a))
	}
	scy := get("scy")
	if scy == "" || scy == "<nil>" {
		scy = "auto"
	}

	out := map[string]interface{}{
		"tag":      "proxy",
		"protocol": "vmess",
		"settings": map[string]interface{}{
			"vnext": []map[string]interface{}{
				{
					"address": get("add"),
					"port":    port,
					"users": []map[string]interface{}{
						{"id": get("id"), "alterId": aid, "security": scy},
					},
				},
			},
		},
	}

	network := get("net")
	if network == "" || network == "<nil>" {
		network = "tcp"
	}
	security := get("tls")
	host := get("host")
	path := get("path")
	sni := get("sni")
	headerType := get("type")

	out["streamSettings"] = buildStream(streamParams{
		network:    network,
		security:   security,
		host:       host,
		path:       path,
		sni:        sni,
		headerType: headerType,
		alpn:       get("alpn"),
	})
	return out, nil
}

// ---- vless ----

func parseVless(raw string) (map[string]interface{}, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	id := u.User.Username()
	port := atoi(u.Port())

	user := map[string]interface{}{
		"id":         id,
		"encryption": orDefault(q.Get("encryption"), "none"),
	}
	if flow := q.Get("flow"); flow != "" {
		user["flow"] = flow
	}

	out := map[string]interface{}{
		"tag":      "proxy",
		"protocol": "vless",
		"settings": map[string]interface{}{
			"vnext": []map[string]interface{}{
				{
					"address": u.Hostname(),
					"port":    port,
					"users":   []map[string]interface{}{user},
				},
			},
		},
		"streamSettings": buildStream(streamFromQuery(q)),
	}
	return out, nil
}

// ---- trojan ----

func parseTrojan(raw string) (map[string]interface{}, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	password := u.User.Username()
	port := atoi(u.Port())

	sp := streamFromQuery(q)
	// trojan به‌صورت پیش‌فرض روی TLS است
	if sp.security == "" {
		sp.security = "tls"
	}

	out := map[string]interface{}{
		"tag":      "proxy",
		"protocol": "trojan",
		"settings": map[string]interface{}{
			"servers": []map[string]interface{}{
				{"address": u.Hostname(), "port": port, "password": password},
			},
		},
		"streamSettings": buildStream(sp),
	}
	return out, nil
}

// ---- shadowsocks ----

func parseShadowsocks(raw string) (map[string]interface{}, error) {
	body := strings.TrimPrefix(raw, "ss://")
	// حذف بخش نام (#...)
	if i := strings.Index(body, "#"); i >= 0 {
		body = body[:i]
	}

	var method, password, host string
	var port int

	if strings.Contains(body, "@") {
		// ss://base64(method:pass)@host:port  یا  ss://method:pass@host:port
		parts := strings.SplitN(body, "@", 2)
		userInfo := parts[0]
		if dec, err := b64decode(userInfo); err == nil && strings.Contains(dec, ":") {
			userInfo = dec
		}
		mp := strings.SplitN(userInfo, ":", 2)
		if len(mp) != 2 {
			return nil, fmt.Errorf("ss userinfo نامعتبر")
		}
		method, password = mp[0], mp[1]
		host, port = splitHostPort(parts[1])
	} else {
		// کل بدنه base64 است: method:pass@host:port
		dec, err := b64decode(body)
		if err != nil {
			return nil, fmt.Errorf("ss base64: %w", err)
		}
		at := strings.SplitN(dec, "@", 2)
		if len(at) != 2 {
			return nil, fmt.Errorf("ss فرمت نامعتبر")
		}
		mp := strings.SplitN(at[0], ":", 2)
		if len(mp) != 2 {
			return nil, fmt.Errorf("ss method:pass نامعتبر")
		}
		method, password = mp[0], mp[1]
		host, port = splitHostPort(at[1])
	}

	out := map[string]interface{}{
		"tag":      "proxy",
		"protocol": "shadowsocks",
		"settings": map[string]interface{}{
			"servers": []map[string]interface{}{
				{"address": host, "port": port, "method": method, "password": password},
			},
		},
	}
	return out, nil
}

func splitHostPort(s string) (string, int) {
	s = strings.TrimSpace(s)
	// حذف path یا query باقی‌مانده
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return s, 0
	}
	return s[:i], atoi(s[i+1:])
}

// ---- stream settings ----

type streamParams struct {
	network    string
	security   string
	host       string
	path       string
	sni        string
	headerType string
	alpn       string
	fp         string
	pbk        string
	sid        string
	serviceN   string
}

func streamFromQuery(q url.Values) streamParams {
	return streamParams{
		network:    q.Get("type"),
		security:   q.Get("security"),
		host:       q.Get("host"),
		path:       q.Get("path"),
		sni:        q.Get("sni"),
		headerType: q.Get("headerType"),
		alpn:       q.Get("alpn"),
		fp:         q.Get("fp"),
		pbk:        q.Get("pbk"),
		sid:        q.Get("sid"),
		serviceN:   q.Get("serviceName"),
	}
}

func buildStream(p streamParams) map[string]interface{} {
	network := orDefault(p.network, "tcp")
	if network == "<nil>" {
		network = "tcp"
	}
	security := p.security
	if security == "1" || security == "true" {
		security = "tls"
	}
	if security == "" || security == "<nil>" || security == "0" {
		security = "none"
	}

	stream := map[string]interface{}{
		"network":  network,
		"security": security,
	}

	switch network {
	case "ws":
		ws := map[string]interface{}{"path": orDefault(p.path, "/")}
		if p.host != "" && p.host != "<nil>" {
			ws["headers"] = map[string]interface{}{"Host": p.host}
		}
		stream["wsSettings"] = ws
	case "grpc":
		stream["grpcSettings"] = map[string]interface{}{
			"serviceName": p.serviceN,
		}
	case "h2", "http":
		h2 := map[string]interface{}{"path": orDefault(p.path, "/")}
		if p.host != "" && p.host != "<nil>" {
			h2["host"] = []string{p.host}
		}
		stream["httpSettings"] = h2
	case "tcp":
		if p.headerType == "http" {
			tcp := map[string]interface{}{
				"header": map[string]interface{}{
					"type": "http",
					"request": map[string]interface{}{
						"path": []string{orDefault(p.path, "/")},
					},
				},
			}
			stream["tcpSettings"] = tcp
		}
	}

	if security == "tls" {
		tls := map[string]interface{}{}
		if p.sni != "" && p.sni != "<nil>" {
			tls["serverName"] = p.sni
		} else if p.host != "" && p.host != "<nil>" {
			tls["serverName"] = p.host
		}
		if p.alpn != "" && p.alpn != "<nil>" {
			tls["alpn"] = strings.Split(p.alpn, ",")
		}
		if p.fp != "" {
			tls["fingerprint"] = p.fp
		}
		stream["tlsSettings"] = tls
	}

	if security == "reality" {
		reality := map[string]interface{}{
			"serverName": p.sni,
			"publicKey":  p.pbk,
			"shortId":    p.sid,
		}
		if p.fp != "" {
			reality["fingerprint"] = p.fp
		}
		stream["realitySettings"] = reality
	}

	return stream
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
