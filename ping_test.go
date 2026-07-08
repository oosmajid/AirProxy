package main

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// TestRealPingRejectsDeadProxy علتِ اصلی باگ را بازتولید می‌کند:
// یک سرور که TCP handshake را کامل می‌کند ولی هیچ پروکسیِ سالمی پشتش نیست
// (دقیقاً مثل edge یک CDN مثل کلادفلر). tcpPing اشتباهاً سبز می‌دهد،
// اما realPing باید آن را رد کند چون واقعاً از داخل پروکسی ترافیک رد می‌کند.
func TestRealPingRejectsDeadProxy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close() // وصل می‌شود ولی هیچ هندشیک پروکسی‌ای انجام نمی‌دهد
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	link := fmt.Sprintf("vless://11111111-1111-1111-1111-111111111111@127.0.0.1:%d?encryption=none&type=tcp&security=none#dead", port)

	// tcpPing فقط reachability را می‌سنجد، پس اشتباهاً موفق می‌شود (خودِ باگ).
	if _, err := tcpPing(link, 2*time.Second); err != nil {
		t.Fatalf("tcpPing باید (اشتباهاً) موفق شود چون پورت باز است: %v", err)
	}

	// realPing باید کانفیگ مرده را رد کند.
	if _, err := realPing(link, 2*time.Second); err == nil {
		t.Fatal("realPing باید برای پروکسیِ مرده‌ای که فقط TCP handshake می‌کند خطا بدهد")
	}
}

func TestPickFreePort(t *testing.T) {
	p, err := pickFreePort()
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	if p <= 0 || p > 65535 {
		t.Fatalf("پورت نامعتبر: %d", p)
	}
	// باید واقعاً قابل bind باشد.
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
	if err != nil {
		t.Fatalf("پورت پیشنهادی قابل استفاده نیست: %v", err)
	}
	l.Close()
}
