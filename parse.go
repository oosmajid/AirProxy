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
	case strings.HasPrefix(raw, "hy2://"), strings.HasPrefix(raw, "hysteria2://"):
		return parseHysteria2(raw)
	case strings.HasPrefix(raw, "wireguard://"), strings.HasPrefix(raw, "wg://"):
		return parseWireguard(raw)
	case strings.HasPrefix(raw, "socks://"):
		return parseSocks(raw)
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
	case strings.HasPrefix(raw, "hy2://"), strings.HasPrefix(raw, "hysteria2://"):
		proto = "Hysteria2"
	case strings.HasPrefix(raw, "wireguard://"), strings.HasPrefix(raw, "wg://"):
		proto = "WireGuard"
	case strings.HasPrefix(raw, "socks://"):
		proto = "SOCKS"
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

	stream, err := buildStream(streamParams{
		network:    get("net"),
		security:   get("tls"),
		host:       get("host"),
		path:       get("path"),
		sni:        get("sni"),
		headerType: get("type"),
		alpn:       get("alpn"),
		fp:         get("fp"),
		serviceN:   get("path"), // در vmess، serviceName ِ gRPC در path می‌آید
		authority:  get("authority"),
		mode:       firstNonEmpty(get("mode"), modeFromType(get("net"), get("type"))),
		extra:      get("extra"),
		seed:       get("path"), // در vmess، seed ِ mKCP در path می‌آید
		ech:        get("ech"),
		pcs:        get("pcs"),
		vcn:        get("vcn"),
		fm:         get("fm"),
	})
	if err != nil {
		return nil, err
	}
	out["streamSettings"] = stream
	return out, nil
}

// modeFromType در vmess، حالت gRPC (gun/multi) و XHTTP در فیلد type می‌آید.
func modeFromType(network, typ string) string {
	if n := clean(network); n == "grpc" || n == "xhttp" || n == "splithttp" {
		return clean(typ)
	}
	return ""
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

	stream, err := buildStream(streamFromQuery(q))
	if err != nil {
		return nil, err
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
		"streamSettings": stream,
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

	stream, err := buildStream(sp)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"tag":      "proxy",
		"protocol": "trojan",
		"settings": map[string]interface{}{
			"servers": []map[string]interface{}{
				{"address": u.Hostname(), "port": port, "password": password},
			},
		},
		"streamSettings": stream,
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

// streamParams همهٔ پارامترهای ترنسپورت/امنیتِ یک لینک اشتراکی (سبک v2rayN) است.
type streamParams struct {
	network    string
	security   string
	host       string
	path       string
	sni        string
	headerType string
	alpn       string
	fp         string
	pbk        string // reality publicKey
	sid        string // reality shortId
	spx        string // reality spiderX
	pqv        string // reality mldsa65Verify
	serviceN   string // grpc serviceName
	authority  string // grpc authority
	mode       string // grpc: gun/multi — xhttp: auto/packet-up/stream-up/stream-one
	extra      string // xhttp extra (JSON)
	seed       string // mkcp seed
	ech        string // echConfigList (base64 یا "domain+https://dns/…")
	pcs        string // pinnedPeerCertSha256
	vcn        string // verifyPeerCertByName
	fm         string // finalmask (JSON) — مثلاً fragment روی TCP
}

func streamFromQuery(q url.Values) streamParams {
	return streamParams{
		network:    q.Get("type"),
		security:   q.Get("security"),
		host:       q.Get("host"),
		path:       q.Get("path"),
		sni:        firstNonEmpty(q.Get("sni"), q.Get("peer")),
		headerType: q.Get("headerType"),
		alpn:       q.Get("alpn"),
		fp:         q.Get("fp"),
		pbk:        q.Get("pbk"),
		sid:        q.Get("sid"),
		spx:        q.Get("spx"),
		pqv:        q.Get("pqv"),
		serviceN:   q.Get("serviceName"),
		authority:  q.Get("authority"),
		mode:       q.Get("mode"),
		extra:      q.Get("extra"),
		seed:       q.Get("seed"),
		ech:        q.Get("ech"),
		pcs:        q.Get("pcs"),
		vcn:        q.Get("vcn"),
		fm:         q.Get("fm"),
	}
}

// clean مقادیر خالی/‏<nil>‏ حاصل از fmt.Sprintf روی فیلدهای ناموجود را حذف می‌کند.
func clean(s string) string {
	s = strings.TrimSpace(s)
	if s == "<nil>" {
		return ""
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if clean(v) != "" {
			return clean(v)
		}
	}
	return ""
}

func buildStream(p streamParams) (map[string]interface{}, error) {
	network := strings.ToLower(orDefault(clean(p.network), "tcp"))
	switch network {
	case "raw":
		network = "tcp"
	case "splithttp":
		network = "xhttp"
	case "mkcp":
		network = "kcp"
	case "websocket":
		network = "ws"
	case "h2", "http", "h3", "quic":
		return nil, fmt.Errorf("ترنسپورت %q در Xray حذف شده (به‌جایش XHTTP)", network)
	}
	security := strings.ToLower(clean(p.security))
	if security == "1" || security == "true" {
		security = "tls"
	}
	if security == "" || security == "0" {
		security = "none"
	}
	host, path := clean(p.host), clean(p.path)

	stream := map[string]interface{}{
		"network":  network,
		"security": security,
	}

	switch network {
	case "ws":
		ws := map[string]interface{}{"path": orDefault(path, "/")}
		if host != "" {
			ws["host"] = host
		}
		stream["wsSettings"] = ws
	case "httpupgrade":
		hu := map[string]interface{}{"path": orDefault(path, "/")}
		if host != "" {
			hu["host"] = host
		}
		stream["httpupgradeSettings"] = hu
	case "xhttp":
		xh := map[string]interface{}{"path": orDefault(path, "/")}
		if host != "" {
			xh["host"] = host
		}
		if m := clean(p.mode); m != "" {
			xh["mode"] = m
		}
		if ex := clean(p.extra); ex != "" {
			var extra map[string]interface{}
			if err := json.Unmarshal([]byte(ex), &extra); err != nil {
				return nil, fmt.Errorf("xhttp extra نامعتبر: %w", err)
			}
			xh["extra"] = extra
		}
		stream["xhttpSettings"] = xh
	case "grpc":
		g := map[string]interface{}{"serviceName": clean(p.serviceN)}
		if a := clean(p.authority); a != "" {
			g["authority"] = a
		}
		if clean(p.mode) == "multi" {
			g["multiMode"] = true
		}
		stream["grpcSettings"] = g
	case "kcp":
		// در Xray جدید header/seed ِ mKCP به finalmask/udp منتقل شده است.
		var masks []interface{}
		if ht := clean(p.headerType); ht != "" && ht != "none" {
			if ht == "wechat-video" {
				ht = "wechat"
			}
			masks = append(masks, map[string]interface{}{"type": "header-" + ht})
		}
		if seed := clean(p.seed); seed != "" {
			masks = append(masks, map[string]interface{}{"type": "mkcp-aes128gcm", "settings": map[string]interface{}{"password": seed}})
		} else {
			masks = append(masks, map[string]interface{}{"type": "mkcp-original"})
		}
		stream["kcpSettings"] = map[string]interface{}{}
		stream["finalmask"] = map[string]interface{}{"udp": masks}
	case "tcp":
		if clean(p.headerType) == "http" {
			req := map[string]interface{}{"path": strings.Split(orDefault(path, "/"), ",")}
			if host != "" {
				req["headers"] = map[string]interface{}{"Host": strings.Split(host, ",")}
			}
			stream["tcpSettings"] = map[string]interface{}{
				"header": map[string]interface{}{"type": "http", "request": req},
			}
		}
	}

	switch security {
	case "tls":
		tls := map[string]interface{}{}
		if sni := firstNonEmpty(p.sni, host); sni != "" {
			tls["serverName"] = sni
		}
		if a := clean(p.alpn); a != "" {
			tls["alpn"] = strings.Split(a, ",")
		}
		if fp := clean(p.fp); fp != "" {
			tls["fingerprint"] = fp
		}
		if ech := clean(p.ech); ech != "" {
			tls["echConfigList"] = ech
		}
		if pcs := clean(p.pcs); pcs != "" {
			tls["pinnedPeerCertSha256"] = pcs
		}
		if vcn := clean(p.vcn); vcn != "" {
			tls["verifyPeerCertByName"] = vcn
		}
		stream["tlsSettings"] = tls
	case "reality":
		reality := map[string]interface{}{
			"serverName":  clean(p.sni),
			"publicKey":   clean(p.pbk),
			"shortId":     clean(p.sid),
			"fingerprint": orDefault(clean(p.fp), "chrome"),
		}
		if spx := clean(p.spx); spx != "" {
			reality["spiderX"] = spx
		}
		if pqv := clean(p.pqv); pqv != "" {
			reality["mldsa65Verify"] = pqv
		}
		stream["realitySettings"] = reality
	case "none":
	default:
		return nil, fmt.Errorf("security ناشناخته: %s", security)
	}

	if fm := clean(p.fm); fm != "" {
		var mask map[string]interface{}
		if err := json.Unmarshal([]byte(fm), &mask); err != nil {
			return nil, fmt.Errorf("finalmask (fm) نامعتبر: %w", err)
		}
		if old, ok := stream["finalmask"].(map[string]interface{}); ok {
			for k, v := range old {
				if _, exists := mask[k]; !exists {
					mask[k] = v
				}
			}
		}
		stream["finalmask"] = mask
	}

	return stream, nil
}

// ---- hysteria2 ----

// parseHysteria2 لینک hy2:// یا hysteria2:// را به اوت‌باند hysteria (نسخهٔ ۲) Xray تبدیل می‌کند.
func parseHysteria2(raw string) (map[string]interface{}, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	auth := u.User.Username()
	if pw, ok := u.User.Password(); ok {
		auth += ":" + pw
	}
	port := atoi(u.Port())
	if port == 0 {
		port = 443
	}

	tls := map[string]interface{}{"alpn": []string{"h3"}}
	if sni := firstNonEmpty(q.Get("sni"), q.Get("peer")); sni != "" {
		tls["serverName"] = sni
	}
	if a := q.Get("alpn"); a != "" {
		tls["alpn"] = strings.Split(a, ",")
	}
	if pcs := firstNonEmpty(q.Get("pcs"), q.Get("pinSHA256")); pcs != "" {
		tls["pinnedPeerCertSha256"] = strings.ReplaceAll(pcs, ":", "")
	}
	if ech := q.Get("ech"); ech != "" {
		tls["echConfigList"] = ech
	}

	stream := map[string]interface{}{
		"network":          "hysteria",
		"security":         "tls",
		"tlsSettings":      tls,
		"hysteriaSettings": map[string]interface{}{"version": 2, "auth": auth},
	}
	fm := map[string]interface{}{}
	if obfs := q.Get("obfs"); obfs == "salamander" {
		fm["udp"] = []interface{}{map[string]interface{}{
			"type": "salamander", "settings": map[string]interface{}{"password": q.Get("obfs-password")},
		}}
	}
	if hop := firstNonEmpty(q.Get("mport"), q.Get("ports")); hop != "" {
		fm["quicParams"] = map[string]interface{}{"udpHop": map[string]interface{}{"ports": hop}}
	}
	if len(fm) > 0 {
		stream["finalmask"] = fm
	}

	return map[string]interface{}{
		"tag":      "proxy",
		"protocol": "hysteria",
		"settings": map[string]interface{}{
			"version": 2,
			"address": u.Hostname(),
			"port":    port,
		},
		"streamSettings": stream,
	}, nil
}

// ---- wireguard ----

// parseWireguard لینک wireguard:// (فرمت v2rayN) را تبدیل می‌کند:
// wireguard://<privateKey>@host:port?publickey=…&address=10.0.0.2/32&reserved=1,2,3&mtu=1280
func parseWireguard(raw string) (map[string]interface{}, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	secret, _ := url.PathUnescape(u.User.Username())
	peer := map[string]interface{}{
		"publicKey": firstNonEmpty(q.Get("publickey"), q.Get("publicKey")),
		"endpoint":  u.Host,
	}
	if psk := firstNonEmpty(q.Get("presharedkey"), q.Get("preSharedKey")); psk != "" {
		peer["preSharedKey"] = psk
	}
	addrs := strings.Split(orDefault(firstNonEmpty(q.Get("address"), q.Get("ip")), "172.16.0.2/32"), ",")
	for i := range addrs {
		addrs[i] = strings.TrimSpace(addrs[i])
	}
	settings := map[string]interface{}{
		"secretKey": secret,
		"address":   addrs,
		"peers":     []map[string]interface{}{peer},
		"mtu":       orInt(atoi(q.Get("mtu")), 1280),
	}
	if r := q.Get("reserved"); r != "" {
		var res []int
		for _, part := range strings.Split(r, ",") {
			res = append(res, atoi(part))
		}
		settings["reserved"] = res
	}
	return map[string]interface{}{"tag": "proxy", "protocol": "wireguard", "settings": settings}, nil
}

// ---- socks ----

// parseSocks لینک socks:// (با user:pass ساده یا base64) را تبدیل می‌کند.
func parseSocks(raw string) (map[string]interface{}, error) {
	body := strings.TrimPrefix(raw, "socks://")
	if i := strings.Index(body, "#"); i >= 0 {
		body = body[:i]
	}
	var user, pass, hostport string
	if at := strings.LastIndex(body, "@"); at >= 0 {
		cred := body[:at]
		if dec, err := b64decode(cred); err == nil && strings.Contains(dec, ":") {
			cred = dec
		} else if un, err := url.QueryUnescape(cred); err == nil {
			cred = un
		}
		cp := strings.SplitN(cred, ":", 2)
		user = cp[0]
		if len(cp) == 2 {
			pass = cp[1]
		}
		hostport = body[at+1:]
	} else if dec, err := b64decode(body); err == nil && strings.Contains(dec, "@") {
		return parseSocks("socks://" + dec)
	} else {
		hostport = body
	}
	host, port := splitHostPort(hostport)
	server := map[string]interface{}{"address": strings.Trim(host, "[]"), "port": port}
	if user != "" {
		server["users"] = []map[string]interface{}{{"user": user, "pass": pass}}
	}
	return map[string]interface{}{
		"tag":      "proxy",
		"protocol": "socks",
		"settings": map[string]interface{}{"servers": []map[string]interface{}{server}},
	}, nil
}

func orInt(n, def int) int {
	if n == 0 {
		return def
	}
	return n
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
