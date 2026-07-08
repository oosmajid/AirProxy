package main

import "strings"

// parseList متن چندخطی را به فهرست ورودی‌های تروشده تبدیل می‌کند؛
// خطوط خالی و خطوط کامنت (#) نادیده گرفته می‌شوند.
func parseList(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// buildRouting بخش routing کانفیگ Xray را از روی قوانین bypass می‌سازد.
// اگر bypass خاموش باشد یا هیچ قانونی تولید نشود nil برمی‌گرداند؛ یعنی رفتار
// دقیقاً مثل قبل می‌ماند (همهٔ ترافیک از پروکسی می‌رود). مقصدهای مطابق قوانین
// به اوت‌باند direct می‌روند.
func buildRouting(b bypassRules) map[string]interface{} {
	if !b.Enabled {
		return nil
	}
	var rules []map[string]interface{}

	// بلاک QUIC (udp/443) تا sniffing و routing مبتنی بر دامنه درست کار کند.
	if b.BlockQUIC {
		rules = append(rules, map[string]interface{}{
			"type": "field", "outboundTag": "block", "network": "udp", "port": "443",
		})
	}
	// دامنه‌های دلخواه → direct
	if len(b.Domains) > 0 {
		rules = append(rules, map[string]interface{}{
			"type": "field", "outboundTag": "direct", "domain": b.Domains,
		})
	}
	// IP/CIDR های دلخواه → direct
	if len(b.IPs) > 0 {
		rules = append(rules, map[string]interface{}{
			"type": "field", "outboundTag": "direct", "ip": b.IPs,
		})
	}
	// ایران: دامنه‌ها (geosite + همهٔ ‎.ir‎) و IP ها → direct
	if b.Iran {
		rules = append(rules, map[string]interface{}{
			"type": "field", "outboundTag": "direct",
			"domain": []string{"geosite:category-ir", "regexp:\\.ir$"},
		})
		rules = append(rules, map[string]interface{}{
			"type": "field", "outboundTag": "direct", "ip": []string{"geoip:ir"},
		})
	}
	// شبکهٔ محلی/خصوصی → direct
	if b.Private {
		rules = append(rules, map[string]interface{}{
			"type": "field", "outboundTag": "direct", "ip": []string{"geoip:private"},
		})
	}

	if len(rules) == 0 {
		return nil
	}
	return map[string]interface{}{
		"domainStrategy": "AsIs",
		"rules":          rules,
	}
}
