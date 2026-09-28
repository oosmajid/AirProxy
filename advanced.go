package main

import "strings"

// advanced تنظیمات هستهٔ سبک v2rayN است که روی همهٔ کانفیگ‌ها اعمال می‌شود
// (هم هنگام اتصال و هم هنگام پینگ، تا پینگ دقیقاً همان مسیر اتصال را بسنجد).
type advanced struct {
	// Fragment: شکستن ClientHello ِ TLS / بسته‌های اول TCP برای عبور از DPI.
	Fragment     bool   `json:"fragment"`
	FragPackets  string `json:"frag_packets"`  // "tlshello" یا بازه مثل "1-3"
	FragLength   string `json:"frag_length"`   // مثل "100-200"
	FragInterval string `json:"frag_interval"` // میلی‌ثانیه، مثل "10-20"

	// Mux (mux.cool) برای تجمیع چند اتصال روی یک تونل.
	Mux            bool `json:"mux"`
	MuxConcurrency int  `json:"mux_concurrency"`

	// TLS
	Fingerprint string `json:"fingerprint"` // uTLS پیش‌فرض برای لینک‌های بدون fp
	ECH         string `json:"ech"`         // echConfigList سراسری برای لینک‌های TLS بدون ech
	ECHForce    string `json:"ech_force"`   // echForceQuery: none | half | full

	TFO            bool   `json:"tfo"`             // TCP Fast Open
	DomainStrategy string `json:"domain_strategy"` // routing: AsIs | IPIfNonMatch | IPOnDemand

	// BindPhysical وقتی یک VPN سیستمی (مثل Happ/v2rayN در حالت TUN) روشن است، ترافیکِ
	// خروجیِ AirProxy را مستقیماً به کارت شبکهٔ فیزیکی می‌بندد تا از داخلِ آن VPN رد نشود.
	// بدون این، پینگ هر کانفیگِ مرده‌ای هم سبز می‌شد چون از مسیر VPN دیگر به سرور می‌رسید.
	BindPhysical bool `json:"bind_physical"`
}

func defaultAdvanced() advanced {
	return advanced{
		FragPackets:    "tlshello",
		FragLength:     "100-200",
		FragInterval:   "10-20",
		MuxConcurrency: 8,
		Fingerprint:    "chrome",
		DomainStrategy: "AsIs",
		BindPhysical:   true,
	}
}

// applyAdvanced تنظیمات پیشرفته را روی کانفیگ کامل Xray (خروجی buildConfig) اعمال می‌کند.
func applyAdvanced(cfg map[string]interface{}, adv advanced) {
	outs, _ := cfg["outbounds"].([]map[string]interface{})
	if len(outs) == 0 {
		return
	}
	proxy := outs[0]
	protocol, _ := proxy["protocol"].(string)
	udpProto := protocol == "hysteria" || protocol == "wireguard"
	local := isLoopback(outboundAddress(proxy)) // مثلاً اوت‌باند socks ِ تونل SSH

	ss, _ := proxy["streamSettings"].(map[string]interface{})
	if ss == nil {
		ss = map[string]interface{}{}
	}
	network, _ := ss["network"].(string)
	security, _ := ss["security"].(string)

	// ---- TLS: fingerprint و ECH ----
	if tls, ok := ss["tlsSettings"].(map[string]interface{}); ok && security == "tls" {
		if _, has := tls["fingerprint"]; !has && adv.Fingerprint != "" && protocol != "hysteria" {
			tls["fingerprint"] = adv.Fingerprint
		}
		if _, has := tls["echConfigList"]; !has && strings.TrimSpace(adv.ECH) != "" {
			tls["echConfigList"] = strings.TrimSpace(adv.ECH)
		}
		if _, has := tls["echConfigList"]; has && adv.ECHForce != "" {
			tls["echForceQuery"] = adv.ECHForce
		}
	}
	if r, ok := ss["realitySettings"].(map[string]interface{}); ok && adv.Fingerprint != "" {
		if fp, _ := r["fingerprint"].(string); fp == "" {
			r["fingerprint"] = adv.Fingerprint
		}
	}

	// ---- Fragment (finalmask/tcp) ----
	if adv.Fragment && !udpProto && !local && network != "kcp" {
		fm, _ := ss["finalmask"].(map[string]interface{})
		if fm == nil {
			fm = map[string]interface{}{}
		}
		var tcp []interface{}
		if old, ok := fm["tcp"].([]interface{}); ok {
			for _, m := range old {
				if mm, ok := m.(map[string]interface{}); ok && mm["type"] == "fragment" {
					continue // تنظیم سراسری جایگزین fragment ِ داخل لینک می‌شود
				}
				tcp = append(tcp, m)
			}
		}
		tcp = append(tcp, map[string]interface{}{
			"type": "fragment",
			"settings": map[string]interface{}{
				"packets": orDefault(adv.FragPackets, "tlshello"),
				"length":  orDefault(adv.FragLength, "100-200"),
				"delay":   orDefault(adv.FragInterval, "10-20"),
			},
		})
		fm["tcp"] = tcp
		ss["finalmask"] = fm
	}

	// ---- sockopt: TFO و bind به کارت شبکهٔ فیزیکی ----
	sockopt, _ := ss["sockopt"].(map[string]interface{})
	if sockopt == nil {
		sockopt = map[string]interface{}{}
	}
	if adv.TFO && !udpProto && !local {
		sockopt["tcpFastOpen"] = true
	}
	iface := ""
	if adv.BindPhysical {
		if name, vpn := physicalInterface(); vpn {
			iface = name
		}
	}
	if iface != "" && !local {
		sockopt["interface"] = iface
		if tls, ok := ss["tlsSettings"].(map[string]interface{}); ok {
			if _, has := tls["echConfigList"]; has {
				tls["echSockopt"] = map[string]interface{}{"interface": iface}
			}
		}
	}
	if len(sockopt) > 0 {
		ss["sockopt"] = sockopt
	}
	if len(ss) > 0 {
		proxy["streamSettings"] = ss
	}

	// ---- Mux ----
	vision := false
	if flow := firstUserField(proxy, "flow"); strings.Contains(flow, "vision") {
		vision = true
	}
	switch protocol {
	case "vmess", "vless", "trojan", "shadowsocks":
		if adv.Mux && !vision && network != "xhttp" {
			proxy["mux"] = map[string]interface{}{
				"enabled":         true,
				"concurrency":     orInt(adv.MuxConcurrency, 8),
				"xudpConcurrency": 16,
				"xudpProxyUDP443": "reject",
			}
		}
	}

	// اوت‌باند direct هم باید از VPN ِ سیستمی عبور نکند (ترافیک bypass شده).
	if iface != "" {
		for _, o := range outs[1:] {
			if o["protocol"] == "freedom" {
				o["streamSettings"] = map[string]interface{}{"sockopt": map[string]interface{}{"interface": iface}}
			}
		}
	}

	if r, ok := cfg["routing"].(map[string]interface{}); ok && adv.DomainStrategy != "" {
		r["domainStrategy"] = adv.DomainStrategy
	}
}

// outboundAddress آدرس سرور یک اوت‌باند را برمی‌گرداند (برای همهٔ پروتکل‌های پشتیبانی‌شده).
func outboundAddress(ob map[string]interface{}) string {
	h, _ := outboundEndpoint(ob)
	return h
}

// outboundEndpoint آدرس و پورت سرور یک اوت‌باند را استخراج می‌کند.
func outboundEndpoint(ob map[string]interface{}) (string, int) {
	settings, _ := ob["settings"].(map[string]interface{})
	if settings == nil {
		return "", 0
	}
	if vnext, ok := settings["vnext"].([]map[string]interface{}); ok && len(vnext) > 0 {
		return asString(vnext[0]["address"]), asInt(vnext[0]["port"])
	}
	if servers, ok := settings["servers"].([]map[string]interface{}); ok && len(servers) > 0 {
		return asString(servers[0]["address"]), asInt(servers[0]["port"])
	}
	if peers, ok := settings["peers"].([]map[string]interface{}); ok && len(peers) > 0 {
		h, p := splitHostPort(asString(peers[0]["endpoint"]))
		return strings.Trim(h, "[]"), p
	}
	if addr, ok := settings["address"].(string); ok {
		return addr, asInt(settings["port"])
	}
	return "", 0
}

func firstUserField(ob map[string]interface{}, key string) string {
	settings, _ := ob["settings"].(map[string]interface{})
	if vnext, ok := settings["vnext"].([]map[string]interface{}); ok && len(vnext) > 0 {
		if users, ok := vnext[0]["users"].([]map[string]interface{}); ok && len(users) > 0 {
			s, _ := users[0][key].(string)
			return s
		}
	}
	return ""
}

func isLoopback(host string) bool {
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}
