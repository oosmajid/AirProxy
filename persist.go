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

// store کل وضعیت ماندگار اپ.
type store struct {
	Sources []string  `json:"sources"`
	Configs []cfgItem `json:"configs"`
	Listen  string    `json:"listen"`
	Socks   int       `json:"socks"`
	HTTP    int       `json:"http"`
	Rotate  bool      `json:"rotate"`
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
	s := store{Listen: "127.0.0.1", Socks: 10808, HTTP: 10809, Rotate: true}
	raw := p.String(prefKey)
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
