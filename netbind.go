package main

import (
	"bufio"
	"bytes"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	physMu      sync.Mutex
	physAt      time.Time
	physName    string
	physVPNOn   bool
	tunPrefixes = []string{"utun", "ipsec", "ppp", "tun", "tap", "wg", "gif", "stf"}
)

func isTunnelIface(name string) bool {
	for _, p := range tunPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// physicalInterface نام کارت شبکهٔ فیزیکیِ دارای مسیر پیش‌فرض (مثل en0) را برمی‌گرداند و
// اینکه آیا در حال حاضر یک VPN ِ سیستمی (مسیر پیش‌فرض روی utun/…) فعال است.
// نتیجه چند ثانیه کش می‌شود چون در پینگ گروهی ده‌ها بار صدا زده می‌شود.
func physicalInterface() (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	physMu.Lock()
	defer physMu.Unlock()
	if time.Since(physAt) < 5*time.Second {
		return physName, physVPNOn
	}
	out, err := exec.Command("/usr/sbin/netstat", "-rn", "-f", "inet").Output()
	if err != nil {
		physName, physVPNOn, physAt = "", false, time.Now()
		return "", false
	}
	physName, physVPNOn = parseDefaultRoutes(out)
	physAt = time.Now()
	return physName, physVPNOn
}

// parseDefaultRoutes خروجی netstat -rn را می‌خواند: اولین مسیر default، مسیر فعال است.
// اگر آن مسیر روی یک اینترفیس تونل باشد یعنی VPN روشن است؛ آنگاه اولین مسیر default
// روی یک اینترفیس غیرتونل، کارت فیزیکی است.
func parseDefaultRoutes(out []byte) (string, bool) {
	var ifaces []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 4 && (f[0] == "default" || f[0] == "0.0.0.0/0") {
			ifaces = append(ifaces, f[3])
		}
	}
	if len(ifaces) == 0 {
		return "", false
	}
	vpn := isTunnelIface(ifaces[0])
	for _, n := range ifaces {
		if !isTunnelIface(n) {
			return n, vpn
		}
	}
	return "", false
}
