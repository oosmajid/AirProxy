package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
)

// TestRoutingE2E اثبات می‌کند که تصمیم routing در زمان اجرا درست کار می‌کند:
// اوت‌باند "proxy" را یک blackhole می‌گذاریم؛ پس هر چیزی که به proxy برود بلاک
// می‌شود و هر چیزی که به direct برود به سرور محلی می‌رسد.
//   - با قانون bypass روی 127.0.0.1/32  → direct → «HELLO» دریافت می‌شود.
//   - بدون هیچ قانونی                    → proxy(blackhole) → بلاک می‌شود.
func TestRoutingE2E(t *testing.T) {
	host, port, stop := startHelloServer(t)
	defer stop()

	// حالت ۱: مقصد مطابق قانون IP سفارشی → باید مستقیم برود و به سرور برسد.
	sp1 := freePort(t)
	inst1 := startXrayInstance(t, sp1, buildRouting(bypassRules{Enabled: true, IPs: []string{"127.0.0.1/32"}}))
	waitForSocks(t, sp1)
	got := socksFetch(fmt.Sprintf("127.0.0.1:%d", sp1), host, port)
	inst1.Close()
	if got != "HELLO" {
		t.Fatalf("matched destination should route DIRECT and read HELLO, got %q", got)
	}

	// حالت ۲: بدون قانون → پیش‌فرض به proxy(blackhole) → باید بلاک شود.
	sp2 := freePort(t)
	inst2 := startXrayInstance(t, sp2, nil)
	waitForSocks(t, sp2)
	got2 := socksFetch(fmt.Sprintf("127.0.0.1:%d", sp2), host, port)
	inst2.Close()
	if got2 == "HELLO" {
		t.Fatalf("unmatched destination must route to proxy(blackhole) and be blocked, but read HELLO")
	}
}

// startXrayInstance یک نمونهٔ Xray با اوت‌باند proxy=blackhole بالا می‌آورد.
func startXrayInstance(t *testing.T, socksPort int, routing map[string]interface{}) *core.Instance {
	t.Helper()
	cfg := buildConfig("127.0.0.1", socksPort, 0,
		map[string]interface{}{"tag": "proxy", "protocol": "blackhole"}, routing)
	jb, _ := json.Marshal(cfg)
	cc, err := serial.LoadJSONConfig(bytes.NewReader(jb))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	inst, err := core.New(cc)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	if err := inst.Start(); err != nil {
		t.Fatalf("instance start: %v", err)
	}
	return inst
}

// startHelloServer یک سرور TCP محلی که «HELLO» می‌فرستد و می‌بندد.
func startHelloServer(t *testing.T) (host string, port int, stop func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("HELLO"))
			c.Close()
		}
	}()
	return "127.0.0.1", l.Addr().(*net.TCPAddr).Port, func() { l.Close() }
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitForSocks منتظر می‌ماند تا پورت SOCKS آمادهٔ اتصال شود.
func waitForSocks(t *testing.T, port int) {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 100; i++ {
		if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socks port %d never became ready", port)
}

// socksFetch از طریق پروکسی SOCKS5 به مقصد وصل می‌شود و حداکثر ۵ بایت می‌خواند.
// در صورت هر خطا رشتهٔ خالی برمی‌گرداند.
func socksFetch(socksAddr, targetHost string, targetPort int) string {
	c, err := net.DialTimeout("tcp", socksAddr, 3*time.Second)
	if err != nil {
		return ""
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(4 * time.Second))

	// greeting: VER=5, NMETHODS=1, METHOD=0 (noauth)
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return ""
	}
	sel := make([]byte, 2)
	if _, err := io.ReadFull(c, sel); err != nil || sel[1] != 0x00 {
		return ""
	}
	// request: VER=5, CMD=1(connect), RSV=0, ATYP=1(ipv4), IP(4), PORT(2)
	ip := net.ParseIP(targetHost).To4()
	req := []byte{0x05, 0x01, 0x00, 0x01}
	req = append(req, ip...)
	req = append(req, byte(targetPort>>8), byte(targetPort))
	if _, err := c.Write(req); err != nil {
		return ""
	}
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil || rep[1] != 0x00 {
		return ""
	}
	buf := make([]byte, 5)
	n, _ := io.ReadFull(c, buf)
	return string(buf[:n])
}
