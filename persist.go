package main

import (
	"encoding/json"
	"strings"

	"fyne.io/fyne/v2"
)

// cfgItem یک کانفیگ در لیست است.
type cfgItem struct {
	Raw     string `json:"raw"`
	Name    string `json:"name"`
	Proto   string `json:"proto"`
	Group   string `json:"group"` // برابر با رشتهٔ منبع (source)
	Latency string `json:"-"`     // ذخیره نمی‌شود
}

// bypassRules قوانین دور زدن پروکسی (split-tunneling) را نگه می‌دارد.
// مقصدهای مطابق این قوانین مستقیم (direct) می‌روند، نه از داخل پروکسی.
type bypassRules struct {
	Enabled   bool     `json:"enabled"`    // کلید اصلی روشن/خاموش
	Iran      bool     `json:"iran"`       // geosite:category-ir + regexp:\.ir$ + geoip:ir
	Private   bool     `json:"private"`    // geoip:private (شبکهٔ محلی)
	BlockQUIC bool     `json:"block_quic"` // بلاک udp/443 تا routing مبتنی بر دامنه درست بماند
	Domains   []string `json:"domains"`    // دامنه‌های دلخواه → direct
	IPs       []string `json:"ips"`        // IP/CIDR های دلخواه → direct
}

// defaultBypass مقدار پیش‌فرض را برمی‌گرداند: bypass روشن با ایران/محلی/بلاک-QUIC.
func defaultBypass() bypassRules {
	return bypassRules{Enabled: true, Iran: true, Private: true, BlockQUIC: true}
}

// store کل وضعیت ماندگار اپ.
type store struct {
	Sources []string    `json:"sources"`
	Configs []cfgItem   `json:"configs"`
	Listen  string      `json:"listen"`
	Socks   int         `json:"socks"`
	HTTP    int         `json:"http"`
	Rotate  bool        `json:"rotate"`
	Bypass  bypassRules `json:"bypass"`
}

// prettyGroup نام نمایشی یک گروه (منبع) را می‌سازد.
func prettyGroup(source string) string {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "ssh://") {
		if c, err := parseSSHLink(source); err == nil && c.host != "" {
			return "SSH · " + c.host
		}
		return "SSH"
	}
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		return "Links"
	}
	s := source
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	return s
}

const prefKey = "data_v1"

func loadStore(p fyne.Preferences) store {
	return decodeStore(p.String(prefKey))
}

// decodeStore رشتهٔ ذخیره‌شده را به store تبدیل می‌کند و مقادیر پیش‌فرض را ست می‌کند.
// چون Bypass پیش از Unmarshal روی defaultBypass() ست می‌شود، دیتای قدیمیِ فاقد
// فیلد bypass به‌صورت خودکار «ایران روشن» می‌شود، ولی اگر کاربر عمداً خاموشش کرده
// باشد همان مقدار ذخیره‌شده اعمال می‌گردد.
func decodeStore(raw string) store {
	s := store{Listen: "127.0.0.1", Socks: 10808, HTTP: 10809, Rotate: true, Bypass: defaultBypass()}
	if raw == "" {
		return s
	}
	_ = json.Unmarshal([]byte(raw), &s)
	if s.Listen == "" {
		s.Listen = "127.0.0.1"
	}
	if s.Socks == 0 {
		s.Socks = 10808
	}
	return s
}

func saveStore(p fyne.Preferences, s store) {
	b, err := json.Marshal(s)
	if err == nil {
		p.SetString(prefKey, string(b))
	}
}
