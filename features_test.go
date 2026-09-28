package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
)

// loadInXray کانفیگ را واقعاً در هستهٔ Xray بارگذاری می‌کند تا از معتبر بودن JSON مطمئن شویم.
func loadInXray(t *testing.T, cfg map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(cfg)
	cc, err := serial.LoadJSONConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("xray rejected config: %v\n%s", err, b)
	}
	if _, err := core.New(cc); err != nil {
		t.Fatalf("core.New: %v\n%s", err, b)
	}
}

func streamOf(t *testing.T, ob map[string]interface{}) map[string]interface{} {
	ss, ok := ob["streamSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("no streamSettings: %v", ob)
	}
	return ss
}

const uuid = "11111111-1111-1111-1111-111111111111"

func TestParseAllLinkTypesLoadInXray(t *testing.T) {
	if _, err := ensureGeoAssets(); err != nil {
		t.Fatalf("geo: %v", err)
	}
	vm, _ := json.Marshal(map[string]interface{}{
		"v": "2", "ps": "vm", "add": "example.com", "port": "443", "id": uuid, "aid": "0",
		"net": "xhttp", "type": "packet-up", "host": "cdn.example.com", "path": "/x", "tls": "tls",
		"sni": "cdn.example.com", "fp": "firefox", "alpn": "h2",
	})
	links := map[string]string{
		"vless-ws-tls-ech":  "vless://" + uuid + "@1.2.3.4:443?security=tls&type=ws&host=a.com&path=%2Fws%3Fed%3D2048&sni=a.com&fp=chrome&ech=cloudflare-ech.com%2Bhttps%3A%2F%2F1.1.1.1%2Fdns-query#n",
		"vless-xhttp-fm":    "vless://" + uuid + "@104.18.8.122:443?encryption=none&security=tls&type=xhttp&host=cdn.a.com&sni=cdn.a.com&fp=chrome&alpn=h2%2Chttp%2F1.1&fm=%7B%22tcp%22%3A%5B%7B%22type%22%3A%22fragment%22%2C%22settings%22%3A%7B%22packets%22%3A%221-3%22%2C%22length%22%3A%22100-200%22%2C%22delay%22%3A%2210-20%22%7D%7D%5D%7D&path=%2Fp&mode=auto&extra=%7B%22xPaddingBytes%22%3A%22100-1000%22%7D#x",
		"vless-reality":     "vless://" + uuid + "@1.2.3.4:443?security=reality&type=tcp&flow=xtls-rprx-vision&sni=www.google.com&pbk=Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw&sid=6ba85179e30d4fc2&spx=%2F#r",
		"vless-grpc-multi":  "vless://" + uuid + "@1.2.3.4:443?security=tls&type=grpc&serviceName=gs&mode=multi&authority=a.com&sni=a.com#g",
		"vless-httpupgrade": "vless://" + uuid + "@1.2.3.4:80?type=httpupgrade&host=a.com&path=%2Fhu#h",
		"vless-kcp":         "vless://" + uuid + "@1.2.3.4:80?type=kcp&headerType=wechat-video&seed=abc#k",
		"vless-raw-http":    "vless://" + uuid + "@1.2.3.4:80?type=raw&headerType=http&host=a.com,b.com&path=%2F#t",
		"vmess-xhttp":       "vmess://" + base64.StdEncoding.EncodeToString(vm),
		"trojan-ws":         "trojan://pw@1.2.3.4:443?type=ws&host=a.com&path=%2Ft&sni=a.com#tr",
		"ss":                "ss://" + base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pw")) + "@1.2.3.4:8388#s",
		"hy2":               "hy2://auth@1.2.3.4:443?sni=a.com&obfs=salamander&obfs-password=ob&mport=20000-30000#hy",
		"wireguard":         "wireguard://" + "cHJpdmF0ZWtleXByaXZhdGVrZXlwcml2YXRla2V5MTI=" + "@1.2.3.4:51820?publickey=cHVibGlja2V5cHVibGlja2V5cHVibGlja2V5MTIzNDU%3D&address=172.16.0.2%2F32&reserved=1,2,3&mtu=1280#wg",
		"socks":             "socks://" + base64.StdEncoding.EncodeToString([]byte("u:p")) + "@1.2.3.4:1080#so",
	}
	adv := defaultAdvanced()
	adv.Fragment, adv.Mux, adv.TFO = true, true, true
	adv.ECH = "cloudflare-ech.com+https://1.1.1.1/dns-query"
	for name, l := range links {
		t.Run(name, func(t *testing.T) {
			ob, err := parseLink(l)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if h, p := outboundEndpoint(ob); h == "" || p == 0 {
				t.Fatalf("no endpoint: %v", ob)
			}
			loadInXray(t, buildConfig("127.0.0.1", 10808, 0, ob, nil))
			// با همهٔ تنظیمات پیشرفته هم باید معتبر بماند.
			ob2, _ := parseLink(l)
			cfg := buildConfig("127.0.0.1", 10808, 0, ob2, buildRouting(defaultBypass()))
			applyAdvanced(cfg, adv)
			loadInXray(t, cfg)
		})
	}
}

func TestXHTTPSettingsParsed(t *testing.T) {
	ob, err := parseLink("vless://" + uuid + "@1.2.3.4:443?security=tls&type=xhttp&host=h.com&path=%2Fp&mode=packet-up&sni=h.com")
	if err != nil {
		t.Fatal(err)
	}
	xh, ok := streamOf(t, ob)["xhttpSettings"].(map[string]interface{})
	if !ok || xh["path"] != "/p" || xh["host"] != "h.com" || xh["mode"] != "packet-up" {
		t.Fatalf("xhttpSettings wrong: %v", streamOf(t, ob))
	}
}

func TestH2TransportGivesClearError(t *testing.T) {
	if _, err := parseLink("vless://" + uuid + "@1.2.3.4:443?type=h2&security=tls"); err == nil {
		t.Fatal("h2 is removed from Xray; parse must fail clearly")
	}
}

func TestApplyAdvancedFragmentECHMux(t *testing.T) {
	ob, _ := parseLink("vless://" + uuid + "@1.2.3.4:443?security=tls&type=ws&host=a.com&sni=a.com")
	cfg := buildConfig("127.0.0.1", 10808, 0, ob, nil)
	adv := defaultAdvanced()
	adv.Fragment, adv.Mux = true, true
	adv.ECH, adv.ECHForce = "cloudflare-ech.com+https://1.1.1.1/dns-query", "half"
	applyAdvanced(cfg, adv)
	ss := streamOf(t, ob)
	tls := ss["tlsSettings"].(map[string]interface{})
	if tls["fingerprint"] != "chrome" || tls["echConfigList"] != adv.ECH || tls["echForceQuery"] != "half" {
		t.Fatalf("tls not applied: %v", tls)
	}
	fm := ss["finalmask"].(map[string]interface{})
	tcp := fm["tcp"].([]interface{})
	if len(tcp) != 1 || tcp[0].(map[string]interface{})["type"] != "fragment" {
		t.Fatalf("fragment not applied: %v", fm)
	}
	if mux, ok := ob["mux"].(map[string]interface{}); !ok || mux["enabled"] != true {
		t.Fatalf("mux not applied: %v", ob["mux"])
	}
}

func TestApplyAdvancedKeepsLinkECHAndSkipsVisionMux(t *testing.T) {
	ob, _ := parseLink("vless://" + uuid + "@1.2.3.4:443?security=tls&type=tcp&flow=xtls-rprx-vision&sni=a.com&fp=safari&ech=AEX%2B")
	cfg := buildConfig("127.0.0.1", 10808, 0, ob, nil)
	adv := defaultAdvanced()
	adv.Mux = true
	adv.ECH = "global.example+https://1.1.1.1/dns-query"
	applyAdvanced(cfg, adv)
	tls := streamOf(t, ob)["tlsSettings"].(map[string]interface{})
	if tls["echConfigList"] != "AEX+" || tls["fingerprint"] != "safari" {
		t.Fatalf("link-level ech/fp must win: %v", tls)
	}
	if _, ok := ob["mux"]; ok {
		t.Fatal("mux must not be enabled with xtls-rprx-vision")
	}
}

func TestParseDefaultRoutes(t *testing.T) {
	withVPN := []byte("Routing tables\n\nInternet:\nDestination Gateway Flags Netif Expire\ndefault link#20 UCSg utun4\ndefault 192.168.1.1 UGScIg en0\n1.1.1.1 link#20 UHWIig utun4\n")
	if n, vpn := parseDefaultRoutes(withVPN); n != "en0" || !vpn {
		t.Fatalf("got %q vpn=%v", n, vpn)
	}
	noVPN := []byte("default 192.168.1.1 UGScg en0\ndefault link#20 UCSIg utun3\n")
	if n, vpn := parseDefaultRoutes(noVPN); n != "en0" || vpn {
		t.Fatalf("got %q vpn=%v", n, vpn)
	}
}

// TestProxyHealthRequires204: یک پروکسیِ «زنده» که به‌جای 204 صفحهٔ 200 برمی‌گرداند
// (مثل fallback ِ nginx یا صفحهٔ فیلترینگ) نباید پینگ سبز بگیرد.
func TestProxyHealthRequires204(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "fallback page")
	}))
	httpPort := ln.Addr().(*net.TCPAddr).Port
	// یک xray که همه‌چیز را به این وب‌سرور redirect می‌کند.
	socks, _ := pickFreePort()
	cfg := buildConfig("127.0.0.1", socks, 0, map[string]interface{}{
		"tag": "proxy", "protocol": "freedom",
		"settings": map[string]interface{}{"redirect": fmt.Sprintf("127.0.0.1:%d", httpPort)},
	}, nil)
	b, _ := json.Marshal(cfg)
	cc, err := serial.LoadJSONConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := core.New(cc)
	if err := inst.Start(); err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	old := pingURL
	pingURL = "http://example.test/generate_204" // HTTP تا redirect به وب‌سرور لوکال برسد
	defer func() { pingURL = old }()
	if _, err := proxyHealth("127.0.0.1", socks, 3*time.Second); err == nil {
		t.Fatal("200 fallback page must not count as a healthy proxy")
	}
}
